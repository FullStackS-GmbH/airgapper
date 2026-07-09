## Architecture Decision Records

### ADR-1: Hexagonal Architecture

**Context**: The tool interacts with multiple external systems (registries, git hosts, scanners) through different protocols.
The Python version mixes transport logic with orchestration.

**Decision**: Adopt hexagonal architecture (ports & adapters).
The domain core defines interfaces; adapters implement them. Dependencies point inward.

**Consequences**:

- Transport backends can be added, replaced, or mocked without touching the core.
- The sync engine is testable in isolation with mock transporters.
- Clear separation between "what to sync" (domain) and "how to sync" (adapters).

### ADR-2: Strategy Pattern for Transports

**Context**: Three artifact types (image, helm, git) require different sync implementations but share the same lifecycle (check existence → scan → pull → push).

**Decision**: Use the Strategy pattern via a single `Transporter` interface.
A factory selects the correct implementation based on resource type.

**Consequences**:

- Adding a new artifact type requires only implementing `Transporter` and registering it in the factory.
- The sync engine does not need to change when new types are added.
- All transports have a consistent interface, making the codebase predictable.

### ADR-3: Generic External Scanner

**Context**: The Python version has tool-specific scanner implementations (NeuVector, Snyk, cnspec).
This couples the tool to specific vendors and requires code changes to add new scanners.

**Decision**: Replace all tool-specific scanners with a single generic external command scanner.
It executes a parameterized shell command, checks the exit code, and captures output.

**Consequences**:

- Any scanner tool can be integrated via configuration alone - no code changes needed.
- The airgapper binary stays vendor-neutral and lightweight.
- Users define their own command templates with placeholders like `{registry}`, `{repository}`, `{tag}`.
- Trade-off: no structured vulnerability parsing (threshold counts). Users handle severity logic in their scanner command/script.

### ADR-4: containers/image v5 for Image Transport

**Context**: The Python version implements raw HTTP calls to the Docker Registry v2 API for pulling and pushing images.
This is fragile, hard to maintain, and misses edge cases.

**Decision**: Use `go.podman.io/image/v5` - the same engine family powering skopeo, podman, and buildah - for all container image operations.

**Consequences**:

- Battle-tested image copy logic handling manifests, multi-arch, layer deduplication, auth challenges, and retries.
- Dramatically less code to maintain compared to a hand-rolled HTTP client.
- Build tags keep the binary CGO-free for the registry operations used by airgapper.
- Slight increase in binary size due to the library's scope.

### ADR-5: Constructor-Based Dependency Injection

**Context**: The application needs to wire together config, credential store, transporters, scanners, and the sync engine.

**Decision**: Use manual constructor injection.
The composition root in `cmd/airgapper/main.go` creates all dependencies and passes them as constructor arguments.
No DI framework.

**Consequences**:

- Explicit, debuggable, zero-magic dependency wiring.
- Easy to understand for new contributors.
- If the dependency graph grows significantly, can migrate to Google Wire (compile-time DI) later.

### ADR-6: slog with JSON for Structured Logging

**Context**: The tool needs structured logging that is machine-parseable (for CI/CD and log aggregators) and human-readable (for CLI usage).

**Decision**: Use `log/slog` from the Go standard library with a JSON handler.
Follow OpenTelemetry semantic conventions for attribute names.

**Consequences**:

- Zero external logging dependency.
- JSON output integrates with log aggregators, ELK, Grafana Loki.
- OpenTelemetry semantics enable future tracing integration without changing log attributes.
- In debug mode, logs are verbose; in normal mode, only Info and above.

### ADR-7: Config File Compatibility

**Context**: Existing users have YAML config files from the Python version.
Breaking the config format would force migration effort.

**Decision**: Maintain backward compatibility with the existing config file structure.
Support the same `resources` array with `type`, `source`, `destination`, `tags`, `versions`, `refs`, `push_mode` fields.
Add new fields (e.g., `scanner_command`) as optional additions.

**Consequences**:

- Existing config files work without modification (for core sync features).
- Scanner config changes from tool-specific to generic command-based (migration needed for scanner users).
- New Go-specific features (regex in tags, dry-run) are additive.
