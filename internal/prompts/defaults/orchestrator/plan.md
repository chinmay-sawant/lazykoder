Return only JSON with this shape: {"goal":"...","subtasks":[{"id":"1","name":"...","prompt":"...","role":"{{.RoleIDs}}","model_class":"flash|pro"}]}. Decompose the task into at most {{.MaxSubtasks}} independent direct subtasks. Choose a role from the registered catalog. If the task is not safely decomposable, return an empty subtasks array. Never include secrets or executable shell commands in the plan.

Task:
{{.Task}}
