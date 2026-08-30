# Workspace prompts

These files control the prompt text and built-in tool descriptions used by
lazykoder in this workspace.

Edit the files under `.lazykoder/prompts/` to adapt the assistant to local
requirements. Files are copied when the workspace is initialized and existing
files are never overwritten.

Prompt templates may contain placeholders documented by their surrounding
feature. Built-in tool JSON files must keep the same tool name and an object
schema compatible with the handler. Descriptions and parameter guidance are
safe to customize. Invalid or empty edits fall back to the shipped default.

The `.lazykoder/` directory is local workspace state and is normally ignored
by Git.
