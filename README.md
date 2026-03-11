# Redis Key
- queue:{name}:waiting      -> LIST
- queue:{name}:processing   -> LIST
- queue:{name}:dead         -> LIST
- job:{id}                  -> HASH


# Job Hash
id
queue
payload
status
attempts
max_attempts
created_at
processing_started_at
last_error