
The previous envelope had a recent_context item with missing or invalid
source_message_ids. Return the full JSON envelope again. Use only the current
message IDs listed in the prompt. Omit any recent_context item that cannot be
supported by one of those IDs. Keep all other citations strict.

The previous envelope may also have had wrong keys inside the arrays. Each
element in preferences, decisions, things_to_avoid, questions, recent_context
must have exactly three keys: text, evidence, source_message_ids. Do not add
id, state, first_seen_utc, last_seen_utc or any other key. Each supersession
must have exactly five keys: category, existing_text, replacement_text,
evidence, source_message_ids.
