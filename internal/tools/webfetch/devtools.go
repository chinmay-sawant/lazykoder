package webfetch

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 requires SHA-1 for Sec-WebSocket-Accept.
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	browserStartupTimeout   = 5 * time.Second
	browserNavigationWait   = 5 * time.Second
	maxDevToolsHTTPBytes    = 256 * 1024
	maxDevToolsMessageBytes = 16 * 1024 * 1024
	devtoolsTargetPoll      = 25 * time.Millisecond
	devtoolsDocumentPoll    = 50 * time.Millisecond
	websocketKeyBytes       = 16
	websocketMaskBytes      = 4
	websocketHeaderBytes    = 14
	websocketFinalBit       = 0x80
	websocketOpcodeText     = 0x1
	websocketOpcodeClose    = 0x8
	websocketOpcodePing     = 0x9
	websocketOpcodePong     = 0xA
	websocketOpcodeContinue = 0x0
	websocketOpcodeMask     = 0x0F
	websocketPayloadMask    = 0x7F
	websocketByteBits       = 8
	websocketShortPayload   = 126
	websocketLongPayload    = 127
	websocketMediumMax      = 65535
	webSocketGUID           = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type devtoolsTarget struct {
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type documentSnapshot struct {
	ReadyState string `json:"readyState"`
	URL        string `json:"url"`
	HTML       string `json:"html"`
}

type browserProcess struct {
	done chan struct{}

	mu  sync.Mutex
	err error
}

func newBrowserProcess(cmd interface{ Wait() error }) *browserProcess {
	process := &browserProcess{done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		process.mu.Lock()
		process.err = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process
}

func (p *browserProcess) wait(ctx context.Context) error {
	select {
	case <-p.done:
		return p.error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *browserProcess) error() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func classifyBrowserRuntimeError(ctx context.Context, process *browserProcess, err error, stderr *limitedBuffer) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return browserError(BrowserCancellation, context.Canceled)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return browserError(BrowserNavigationTimeout, context.DeadlineExceeded)
	}
	if processErr := process.error(); processErr != nil {
		return browserError(BrowserRendererCrash, withBrowserStderr(processErr, stderr))
	}
	return browserError(BrowserStartupFailure, withBrowserStderr(err, stderr))
}

func withBrowserStderr(err error, stderr *limitedBuffer) error {
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(stderr.String())
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, truncateRunes(message, maxBrowserErrorMessageRunes))
}

func connectDevTools(ctx context.Context, profile string, process *browserProcess) (*devtoolsClient, error) {
	port, err := waitForDevToolsPort(ctx, profile, process)
	if err != nil {
		return nil, err
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, browserStartupTimeout)
	defer cancel()
	for {
		targets, err := devtoolsTargets(deadlineCtx, port)
		if err == nil {
			for _, target := range targets {
				if target.Type != "page" || target.WebSocketDebuggerURL == "" {
					continue
				}
				return dialDevTools(deadlineCtx, ctx, target.WebSocketDebuggerURL, port)
			}
		}
		select {
		case <-process.done:
			if processErr := process.error(); processErr != nil {
				return nil, processErr
			}
			return nil, fmt.Errorf("browser exited before opening a page target")
		case <-deadlineCtx.Done():
			return nil, fmt.Errorf("wait for browser page target: %w", deadlineCtx.Err())
		case <-time.After(devtoolsTargetPoll):
		}
	}
}

func waitForDevToolsPort(ctx context.Context, profile string, process *browserProcess) (int, error) {
	deadlineCtx, cancel := context.WithTimeout(ctx, browserStartupTimeout)
	defer cancel()
	path := filepath.Join(profile, "DevToolsActivePort")
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			port, parseErr := strconv.Atoi(strings.TrimSpace(lines[0]))
			if parseErr == nil && port > 0 && port <= 65535 {
				return port, nil
			}
			if parseErr != nil {
				return 0, fmt.Errorf("parse DevToolsActivePort: %w", parseErr)
			}
			return 0, fmt.Errorf("parse DevToolsActivePort: invalid port %d", port)
		}
		select {
		case <-process.done:
			if processErr := process.error(); processErr != nil {
				return 0, processErr
			}
			return 0, fmt.Errorf("browser exited before opening DevTools")
		case <-deadlineCtx.Done():
			return 0, fmt.Errorf("wait for DevToolsActivePort: %w", deadlineCtx.Err())
		case <-time.After(devtoolsTargetPoll):
		}
	}
}

func devtoolsTargets(ctx context.Context, port int) ([]devtoolsTarget, error) {
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/json/list"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DevTools returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDevToolsHTTPBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxDevToolsHTTPBytes {
		return nil, fmt.Errorf("DevTools target list exceeds %d bytes", maxDevToolsHTTPBytes)
	}
	var targets []devtoolsTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("decode DevTools targets: %w", err)
	}
	return targets, nil
}

func dialDevTools(dialCtx, clientCtx context.Context, rawURL string, port int) (*devtoolsClient, error) {
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse DevTools websocket URL: %w", err)
	}
	if endpoint.Scheme != "ws" || endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("DevTools websocket is not a local ws endpoint")
	}
	if endpoint.Port() != strconv.Itoa(port) {
		return nil, fmt.Errorf("DevTools websocket port does not match the local endpoint")
	}
	dialer := &net.Dialer{}
	connection, err := dialer.DialContext(dialCtx, "tcp", endpoint.Host)
	if err != nil {
		return nil, fmt.Errorf("dial DevTools websocket: %w", err)
	}
	client, err := newDevToolsClient(clientCtx, connection, endpoint)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return client, nil
}

type devtoolsClient struct {
	connection net.Conn
	reader     *bufio.Reader

	writeMu   sync.Mutex
	closeOnce sync.Once
	nextID    int64
}

func newDevToolsClient(ctx context.Context, connection net.Conn, endpoint *url.URL) (*devtoolsClient, error) {
	keyBytes := make([]byte, websocketKeyBytes)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("create DevTools websocket key: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Host", endpoint.Host)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Sec-WebSocket-Key", key)
	request.Header.Set("Sec-WebSocket-Version", "13")
	if err := request.Write(connection); err != nil {
		return nil, fmt.Errorf("write DevTools websocket handshake: %w", err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, fmt.Errorf("read DevTools websocket handshake: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		_ = response.Body.Close()
		return nil, fmt.Errorf("DevTools websocket returned status %d", response.StatusCode)
	}
	accept := response.Header.Get("Sec-WebSocket-Accept")
	digest := sha1.Sum([]byte(key + webSocketGUID)) //nolint:gosec // RFC 6455 handshake hash.
	wantAccept := base64.StdEncoding.EncodeToString(digest[:])
	if accept != wantAccept {
		return nil, errors.New("DevTools websocket handshake has an invalid accept key")
	}
	client := &devtoolsClient{connection: connection, reader: reader}
	go func() {
		<-ctx.Done()
		client.Close()
	}()
	return client, nil
}

func (c *devtoolsClient) Close() {
	c.closeOnce.Do(func() {
		_ = c.connection.Close()
	})
}

func (c *devtoolsClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.writeMu.Lock()
	c.nextID++
	id := c.nextID
	c.writeMu.Unlock()
	payload, err := json.Marshal(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	})
	if err != nil {
		return nil, fmt.Errorf("encode DevTools %s: %w", method, err)
	}
	if err := c.writeFrame(websocketOpcodeText, payload); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		opcode, message, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if opcode != websocketOpcodeText {
			continue
		}
		var envelope struct {
			ID    int64 `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			continue
		}
		if envelope.ID != id {
			continue
		}
		if envelope.Error != nil {
			return nil, fmt.Errorf("DevTools %s: %s", method, envelope.Error.Message)
		}
		return envelope.Result, nil
	}
}

func (c *devtoolsClient) evaluate(ctx context.Context, expression string) (documentSnapshot, error) {
	result, err := c.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return documentSnapshot{}, err
	}
	var response struct {
		Result struct {
			Value            json.RawMessage `json:"value"`
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		return documentSnapshot{}, fmt.Errorf("decode DevTools evaluation: %w", err)
	}
	if len(response.Result.ExceptionDetails) > 0 && string(response.Result.ExceptionDetails) != "null" {
		return documentSnapshot{}, errors.New("page evaluation failed")
	}
	var snapshot documentSnapshot
	if err := json.Unmarshal(response.Result.Value, &snapshot); err != nil {
		return documentSnapshot{}, fmt.Errorf("decode rendered document: %w", err)
	}
	return snapshot, nil
}

func (c *devtoolsClient) waitForDocument(ctx context.Context, process *browserProcess) (documentSnapshot, error) {
	if _, err := c.call(ctx, "Page.enable", nil); err != nil {
		return documentSnapshot{}, err
	}
	if _, err := c.call(ctx, "Runtime.enable", nil); err != nil {
		return documentSnapshot{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, browserNavigationWait)
	defer cancel()
	const expression = `(() => ({readyState: document.readyState, url: location.href, html: document.documentElement ? document.documentElement.outerHTML : ""}))()`
	for {
		snapshot, err := c.evaluate(waitCtx, expression)
		if err == nil && snapshot.ReadyState == "complete" && snapshot.URL != "" && snapshot.URL != "about:blank" {
			settle := time.NewTimer(browserSettleDelay)
			select {
			case <-settle.C:
			case <-waitCtx.Done():
				if !settle.Stop() {
					<-settle.C
				}
				return documentSnapshot{}, waitCtx.Err()
			case <-process.done:
				if processErr := process.error(); processErr != nil {
					return documentSnapshot{}, processErr
				}
				return documentSnapshot{}, errors.New("browser exited before document capture")
			}
			return c.evaluate(waitCtx, expression)
		}
		select {
		case <-waitCtx.Done():
			return documentSnapshot{}, waitCtx.Err()
		case <-process.done:
			if processErr := process.error(); processErr != nil {
				return documentSnapshot{}, processErr
			}
			return documentSnapshot{}, errors.New("browser exited before document capture")
		case <-time.After(devtoolsDocumentPoll):
		}
	}
}

func (c *devtoolsClient) writeFrame(opcode byte, payload []byte) error {
	if len(payload) > maxDevToolsMessageBytes {
		return fmt.Errorf("DevTools message exceeds %d bytes", maxDevToolsMessageBytes)
	}
	mask := make([]byte, websocketMaskBytes)
	if _, err := rand.Read(mask); err != nil {
		return fmt.Errorf("create websocket mask: %w", err)
	}
	frame := make([]byte, 0, len(payload)+websocketHeaderBytes)
	frame = append(frame, websocketFinalBit|opcode)
	switch {
	case len(payload) < websocketShortPayload:
		frame = append(frame, websocketFinalBit|byte(len(payload)))
	case len(payload) <= websocketMediumMax:
		frame = append(frame, websocketFinalBit|websocketShortPayload, byte(len(payload)>>websocketByteBits), byte(len(payload)))
	default:
		frame = append(frame, websocketFinalBit|websocketLongPayload)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(payload)))
		frame = append(frame, size[:]...)
	}
	frame = append(frame, mask...)
	for index, value := range payload {
		frame = append(frame, value^mask[index%len(mask)])
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.connection.Write(frame)
	return err
}

func (c *devtoolsClient) readMessage() (byte, []byte, error) {
	var message []byte
	var opcode byte
	for {
		final, frameOpcode, payload, err := c.readFrame()
		if err != nil {
			return 0, nil, err
		}
		switch frameOpcode {
		case websocketOpcodeClose:
			return 0, nil, errors.New("DevTools websocket closed")
		case websocketOpcodePing:
			if err := c.writeFrame(websocketOpcodePong, payload); err != nil {
				return 0, nil, err
			}
			continue
		case websocketOpcodeText:
			opcode = frameOpcode
		case websocketOpcodeContinue:
			if opcode == 0 {
				return 0, nil, errors.New("invalid websocket continuation frame")
			}
		default:
			continue
		}
		if len(message)+len(payload) > maxDevToolsMessageBytes {
			return 0, nil, fmt.Errorf("DevTools message exceeds %d bytes", maxDevToolsMessageBytes)
		}
		message = append(message, payload...)
		if final {
			return opcode, message, nil
		}
	}
}

func (c *devtoolsClient) readFrame() (bool, byte, []byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(c.reader, header[:]); err != nil {
		return false, 0, nil, err
	}
	final := header[0]&websocketFinalBit != 0
	opcode := header[0] & websocketOpcodeMask
	masked := header[1]&websocketFinalBit != 0
	length := uint64(header[1] & websocketPayloadMask)
	switch length {
	case websocketShortPayload:
		var size [2]byte
		if _, err := io.ReadFull(c.reader, size[:]); err != nil {
			return false, 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(size[:]))
	case websocketLongPayload:
		var size [8]byte
		if _, err := io.ReadFull(c.reader, size[:]); err != nil {
			return false, 0, nil, err
		}
		length = binary.BigEndian.Uint64(size[:])
	}
	if length > maxDevToolsMessageBytes {
		return false, 0, nil, fmt.Errorf("websocket frame exceeds %d bytes", maxDevToolsMessageBytes)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.reader, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%len(mask)]
		}
	}
	return final, opcode, payload, nil
}
