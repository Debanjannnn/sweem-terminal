# proto

Single cross-language contract for sweem-terminal: domain types, the HTTP route
table, and NATS JetStream event payloads. Go, Rust, and TypeScript all generate
from these files, so a route or payload can never drift between services.

Regenerate from the repo root:

    buf generate proto --template proto/buf.gen.yaml

Output lands in `internal/shared/gen` (Go), `executor/src/gen` (Rust),
`client/src/gen` and `oracle/src/gen` (TS). Generated files are committed and
never hand-edited: change the `.proto`, regenerate, commit both.
