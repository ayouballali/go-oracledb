# TTC cookie connection establishment

TTC cookie optimization reduces negotiation round trips for new physical
connections to a database whose negotiated information is already cached.
It supports the driver's password and token authentication paths without
changing the `database/sql` API.

## Configuration

`OracleDriverProperties.TTCCookieOptimizationEnabled` defaults to `true`.
To disable both cookie selection and cache population:

```go
config := oracle.NewOracleDriverConfig()
config.DriverProperties.TTCCookieOptimizationEnabled = false
```

The flag alone does not select the optimized path. The network session must
also provide a nonblank database UUID, and the cache must contain an entry for
that UUID. Otherwise the driver uses normal negotiation.

## Cache lifecycle

The process-wide `databaseNegotiatedInformation` safe LRU cache holds up to
50 entries, keyed by the raw database UUID received in the network ACCEPT
packet. It stores negotiation metadata, not usernames, passwords, or tokens.
Each entry contains:

- Connection protocol version
- Database character set and character-set flag
- Database NCHAR character set
- Database runtime and compile-time capabilities
- Database platform description (`portage`)

Capability and portage slices are copied when creating an entry and when
reconstructing a protocol object for a connection. Connections do not receive
the cached slices directly.

For eligible connections, the normal instantiator stores the cookie after
successful PRO/DTY negotiation and before authentication. Authentication
failure therefore does not invalidate successfully negotiated metadata.
Successful normal negotiation after a rejected cookie replaces that UUID's
entry. Failed negotiation does not replace it. LRU eviction removes old entries;
the cache does not have a time-based expiry or persist across processes.

## Connection flows

On a cache miss, the normal negotiator exchanges TTIPRO and TTIDTY, installs
the negotiated client configuration, and returns a cookie for the instantiator
to cache when eligible. Normal authentication then runs.

On a cache hit, the cookie negotiator reconstructs protocol state, configures
the client using cached values, and queues TTIPRO, TTICOOKIE, and TTIDTY without
flushing. The cookie authenticator appends OSESSKEY for password authentication
or OAUTH for token authentication and flushes the combined queue.

The shared cookie response reader accepts delayed TTIPRO/TTIDTY messages while
reading authentication responses. A DTY pre-decode callback supplies the
capabilities required for decoding; its post-decode callback stores the returned
time-zone version. A successful OER is checked for a server error, and an RPA
must also have been received before the exchange succeeds.

For passwords, successful OSESSKEY processing updates the session properties;
shared password code then builds and completes OAUTH. For tokens, successful
OAUTH processing updates the session properties directly.

## Rejected cookies and normal fallback

When the server returns TTIRENEG, the cookie reader returns `ErrTTIRENEG`.
The optimized instantiator removes its negotiation callbacks on return.
The connector catches that signal and requests one normal instantiator in
renegotiation mode, using the same network session.

In this mode the normal negotiator reads the server's pending replacement
TTIPRO, rather than sending another client TTIPRO. It reuses normal datatype
negotiation and client configuration, refreshes the eligible cache entry, and
runs normal authentication. Other optimized-path errors do not trigger this
fallback. A failure from the fallback is returned, and unsuccessful connection
establishment follows the connector's network-session cleanup path.

## Internal ownership

| Component | Responsibility |
| --- | --- |
| Connection-instantiator factory | Eligibility, cache lookup, and selection of normal/cookie/recovery construction |
| `ConnectionInstantiatorSetup` | Shared per-attempt construction and authentication dependencies |
| `negotiationSetup` | Shelf, both marshallers, streamer, factories, and negotiated client configuration |
| Cookie negotiator | Cached protocol preparation, negotiation queuing, and DTY callbacks |
| Cookie password/token authenticators | Credential-specific first request and authentication completion |
| Cookie response reader | RPA/OER/warning handling and forwarding the TTIRENEG fallback signal |
| Connector | One fallback on TTIRENEG and network-session lifetime |

Both marshallers use the same connection buffer. The shelf registers the
universal-configured engine as the default and the native engine for TTIPRO,
TTICOOKIE, and TTIDTY. Streamer payloads use
`GetMarshallerForMessageType`; header and flush operations use the default.
No direct serialization bypasses the streamer's outgoing queue.

The existing LOB-prefetch configuration ordering remains a separate follow-up;
this feature does not change when connection properties reach the shelf.

Internal sequence diagrams:

- [Connection selection and fallback](ttc_cookie_connection.puml)
- [Normal and cookie negotiation](ttc_cookie_negotiation.puml)

## Verification

Run the registered unit tests with race detection:

```sh
go test -race -shuffle=on ./... -run '^TestCategoryExecutor$' -args -test.category=unitary
```

Run the configured database recovery test directly:

```sh
go test ./oracle -run '^TestDriver_TTCCookieTTIRENEG$' -count=1 \
  -driver.config.filename=/path/to/test-config.json -driver.config.name=my_database
```

The functional test checks normal cache population, a deliberately stale cookie,
TTIRENEG fallback without a second client PRO, and later acceptance of the
replacement cookie. `TestDriver_TTCCookieConnectionTiming` alternates warmed
physical connections with and without optimization; timing is not a fixed
performance guarantee.

`TestDriver_TTCCookieOCIToken` requires a real configured token provider through
`ORACLE_GO_OCI_TOKEN_CONNECT_DESCRIPTOR`, `ORACLE_GO_OCI_TOKEN_LOCATION`, and
`ORACLE_GO_OCI_TOKEN_EXPECTED_USER`. Missing configuration skips the token test;
an expired token must be renewed before using it as authentication evidence.
