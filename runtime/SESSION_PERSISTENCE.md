# Session persistence

`OpenSession` serializes each session's load/run/checkpoint cycle within one
process. Session identity includes the session, agent and task IDs; filenames
use a hash of that identity. Empty storage directories select ephemeral sessions.

The existing `<hash>.json` file remains the snapshot path. Plain JSON snapshots
from earlier versions are readable and migrate at the next checkpoint. New
snapshots contain a format version, sequence and SHA-256 checksum. Session files
are created with mode 0600 and directories with mode 0700.

Checkpoints append checksummed records to `<hash>.json.jsonl`. A record replaces
only the changed history suffix and changed metadata keys, and carries usage
when it changes. Unchanged checkpoints do not write. Each append is synced
before dirty state is cleared. After 64 records or 4 MiB of log data, a full
snapshot is written, synced and atomically renamed before the log is rotated.
Directory entries are synced on Unix when files are created or replaced.

On resume, only records newer than the snapshot sequence are replayed. This
makes a crash between snapshot replacement and log rotation safe. An incomplete,
unterminated final log record is discarded and the log is truncated to its last
committed newline. Checksummed corruption, identity mismatches and sequence gaps
in committed records fail explicitly; they never silently reset a conversation.
Temporary snapshot files left by a crash are ignored.

Snapshots and logs represent the model's current working history, including
context compaction. The application's activity database remains the full audit
trail. Model-visible tool results and approved argument updates are checkpointed
at their existing lifecycle boundaries; persistence does not make external tool
side effects transactional.

Multiple server processes must not write the same session directory concurrently.
The session mutex is process-local. Windows retains file syncing and atomic
replacement, but directory syncing is unavailable through `os.File` there.
