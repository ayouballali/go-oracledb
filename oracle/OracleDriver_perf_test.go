/*
** Copyright (c) 2026 Oracle and/or its affiliates.
**
** The Universal Permissive License (UPL), Version 1.0
**
** Subject to the condition set forth below, permission is hereby granted to any
** person obtaining a copy of this software, associated documentation and/or data
** (collectively the "Software"), free of charge and under any and all copyright
** rights in the Software, and any and all patent rights owned or freely
** licensable by each licensor hereunder covering either (i) the unmodified
** Software as contributed to or provided by such licensor, or (ii) the Larger
** Works (as defined below), to deal in both
**
** (a) the Software, and
** (b) any piece of software and/or hardware listed in the lrgrwrks.txt file if
** one is included with the Software (each a "Larger Work" to which the Software
** is contributed by such licensors),
**
** without restriction, including without limitation the rights to copy, create
** derivative works of, display, perform, and distribute the Software and make,
** use, sell, offer for sale, import, export, have made, and have sold the
** Software and the Larger Work(s), and to sublicense the foregoing rights on
** either these or other terms.
**
** This license is subject to the following condition:
** The above copyright notice and either this complete permission notice or at
** a minimum a reference to the UPL must be included in all copies or
** substantial portions of the Software.
**
** THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
** IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
** FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
** AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
** LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
** OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
** SOFTWARE.
 */

package oracle

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	"github.com/oracle/go-oracledb/v26/internal/driver/ttc"
	oracleconfig "github.com/oracle/go-oracledb/v26/oracle/config"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

// BenchmarkPoolOpen measures the cost of checking out a pooled database
// connection and returning it to the pool on each iteration.
func BenchmarkPoolOpen(b *testing.B) {
	if TestingConfig == nil {
		b.Skip("No configuration available")
	}

	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("error opening db: %s", err.Error())
		b.Fail()
	} else {
		for b.Loop() {
			conn, _ := db.Conn(context.Background())
			b.StopTimer()
			conn.Close()
			b.StartTimer()
		}
	}
}

// BenchmarkFullPooledConnectionCycle measures a pooled connection checkout
// followed by a ping before the connection is returned to the pool.
func BenchmarkFullPooledConnectionCycle(b *testing.B) {
	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("error opening db: %s", err.Error())
		b.Fail()
	} else {
		for b.Loop() {
			conn, _ := db.Conn(context.Background())
			b.StopTimer()
			conn.PingContext(context.Background())
			b.StartTimer()
			conn.Close()
		}
	}
}

// BenchmarkPoolOpenOneCnx measures pooled connection checkout when the pool
// is constrained to a single open connection.
func BenchmarkPoolOpenOneCnx(b *testing.B) {
	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("error opening db: %s", err.Error())
		b.Fail()
	} else {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)
		b.ResetTimer()
		for b.Loop() {
			conn, _ := db.Conn(context.Background())
			b.StopTimer()
			conn.Close()
			b.StartTimer()
		}
	}
}

// BenchmarkPoolOpenWithWarmOneCnx measures single-connection pool checkout
// after priming the pool with one already-created connection.
func BenchmarkPoolOpenWithWarmOneCnx(b *testing.B) {
	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("error opening db: %s", err.Error())
		b.Fail()
	} else {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		db.SetConnMaxLifetime(0)
		db.SetConnMaxIdleTime(0)

		// make sure we create one
		c, _ := db.Conn(context.Background())
		c.Close()

		b.ResetTimer()
		for b.Loop() {
			conn, _ := db.Conn(context.Background())
			b.StopTimer()
			conn.Close()
			b.StartTimer()
		}
	}
}

// BenchmarkSimpleOpen measures direct connector connection creation and
// close without using the sql.DB pool.
func BenchmarkSimpleOpen(b *testing.B) {
	if TestingConfig == nil {
		b.Skip("No configuration available")
	}

	connector, err := openTestConnectorWithConfig(TestingConfig)
	if err != nil {
		b.Logf("error opening db: %s", err.Error())
		b.Fail()
	} else {
		b.ResetTimer()
		for b.Loop() {
			conn, _ := connector.Connect(context.Background())
			b.StopTimer()
			conn.Close()
			b.StartTimer()
		}
	}
}

// BenchmarkSimpleSelectDual measures repeated execution of SELECT 1 FROM
// DUAL on a single checked-out connection.
func BenchmarkSimpleSelectDual(b *testing.B) {
	var val int

	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("Error opening connection: %s", err.Error())
		b.Fail()
	} else {
		defer db.Close()
		conn, _ := db.Conn(context.Background())
		b.ResetTimer()
		for b.Loop() {
			rows, _ := conn.QueryContext(context.Background(), "SELECT 1 FROM DUAL")
			rows.Next()
			_ = rows.Scan(&val)
			rows.Close()
		}
		conn.Close()
	}

}

// BenchmarkSimpleSelect measures repeated scanning of rows from a simple
// two-column table using a single checked-out connection.
func BenchmarkSimpleSelect(b *testing.B) {
	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Logf("Error opening connection: %s", err.Error())
		b.Fail()
	} else {
		defer db.Close()

		cols := map[string]string{
			"id":   "number",
			"name": "varchar2(100)",
		}
		createTable(context.Background(), db, "t_select", cols)
		defer func() {
			dropTable(context.Background(), db, "t_select")
		}()

		db.ExecContext(context.Background(), "INSERT INTO t_select (id, name) VALUES(42, 'answer')")
		var id int64
		var name string
		conn, _ := db.Conn(context.Background())
		b.ResetTimer()
		for b.Loop() {
			rows, _ := conn.QueryContext(context.Background(), "select id, name from t_select")
			for rows.Next() {
				rows.Scan(&id, &name)
			}
			rows.Close()
		}
		conn.Close()
	}

}

// BenchmarkInsertMultipleValues measures repeated inserts that bind several
// Oracle data types through one checked-out connection.
func BenchmarkInsertMultipleValues(b *testing.B) {
	if TestingConfig == nil {
		b.Skip("No configuration available")
	}

	db, err := openTestDBWithConfig(TestingConfig)
	if err != nil {
		b.Fail()
	} else {
		defer db.Close()

		cols := map[string]string{
			"id":      "NUMBER PRIMARY KEY",
			"n_col":   "NUMBER",
			"v_col":   "VARCHAR2(100)",
			"f_col":   "BINARY_DOUBLE",
			"d_col":   "DATE",
			"ts_col":  "TIMESTAMP(6)",
			"tzt_col": "TIMESTAMP(6) WITH TIME ZONE",
			"b_col":   "BOOLEAN",
			"r_col":   "RAW(2000)",
		}

		tname := createObjectName("execctx_100k_types_test1")
		createTable(context.Background(), db, tname, cols)
		defer func() {
			dropTable(context.Background(), db, tname)
		}()

		insSQL := "INSERT INTO execctx_100k_types_test1 (id, n_col, v_col, f_col, d_col, ts_col, tzt_col, b_col, r_col) " +
			"VALUES (:id, :n, :v, :f, :d, :ts, :tzt, :b, :r)"

		baseDate := time.Date(2024, time.January, 15, 0, 0, 0, 0, time.UTC)
		baseTS := time.Date(2024, time.January, 15, 10, 20, 30, 123456000, time.UTC)
		baseTSTZ := time.Date(2024, time.January, 15, 4, 50, 30, 123456000, time.FixedZone("+05:30", 5*3600+30*60))
		raw := []byte{0xDE, 0xAD, 0xBE, 0xEF}

		var dummyInt int64 = 1

		// wait for currors leak to be fixed
		conn, _ := db.Conn(context.Background())

		b.ResetTimer()
		for b.Loop() {
			// protect ourselves form cursor starvation

			conn.ExecContext(context.Background(), insSQL,
				sql.Named("id", dummyInt),
				sql.Named("n", dummyInt*10),
				sql.Named("v", "bulk"),
				sql.Named("f", float64(dummyInt)*1.01),
				sql.Named("d", baseDate),
				sql.Named("ts", baseTS),
				sql.Named("tzt", baseTSTZ),
				sql.Named("b", (dummyInt%2) == 0),
				sql.Named("r", raw),
			)

			dummyInt++
		}
		conn.Close()
	}
}

func TestDriver_TTCCookieConnectionTiming(t *testing.T) {
	if TestingConfig == nil {
		t.Skip("No configuration available")
	}

	const samples = 20
	type connectionTimingCase struct {
		label        string
		connector    *connector
		eligible     bool
		cacheHit     bool
		renegotiated bool
		total        time.Duration
	}

	newTimingCase := func(label string, enabled bool) *connectionTimingCase {
		t.Helper()
		config := NewOracleDriverConfig()
		config.ConnectDescriptor = TestingConfig.GetConnectionString()
		config.DriverProperties.TTCCookieOptimizationEnabled = enabled
		c, err := NewOracleConnector(config)
		if err != nil {
			t.Fatalf("create %s connector: %v", label, err)
		}
		conn, ok := c.(*connector)
		if !ok {
			t.Fatalf("unexpected connector type: %T", c)
		}
		if conn.connectorConfig.DriverProperties.TTCCookieOptimizationEnabled != enabled {
			t.Fatalf("%s cookie setting was overridden by external configuration", label)
		}

		testCase := &connectionTimingCase{label: label, connector: conn}
		factory := conn.connInstantiatorFactory
		// Observe the existing selection conditions and fallback without replacing
		// the real factory or adding production tracing/cache-reset hooks.
		conn.connInstantiatorFactory = func(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providers common.Registry[oracleProviders.Provider], reneg bool) (driverCommon.ConnectionInstantiator, error) {
			if reneg {
				testCase.renegotiated = true
			} else {
				databaseUUID := ns.GetDatabaseUUID()
				testCase.eligible = ttc.IsTTCCookieEligible(config.DriverProperties, databaseUUID)
				if testCase.eligible {
					_, testCase.cacheHit = ttc.GetCachedTTCCookie(databaseUUID)
				}
			}
			return factory(config, ns, providers, reneg)
		}
		return testCase
	}

	connectAndClose := func(testCase *connectionTimingCase) time.Duration {
		t.Helper()
		testCase.eligible = false
		testCase.cacheHit = false
		testCase.renegotiated = false
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Time a fresh physical connection, without sql.DB pooling or an extra Ping.
		start := time.Now()
		conn, err := testCase.connector.Connect(ctx)
		elapsed := time.Since(start)
		if err != nil {
			// Connect cleans up failed attempts; only close successful connections.
			t.Fatalf("%s connect failed after %v: %v", testCase.label, elapsed, err)
		}
		// Connection teardown is deliberately outside the measured interval.
		if err := conn.Close(); err != nil {
			t.Fatalf("%s close: %v", testCase.label, err)
		}
		return elapsed
	}

	baseline := newTimingCase("without cookie", false)
	optimized := newTimingCase("with cookie", true)

	// Warm both paths. The first enabled connection may populate the cache or
	// refresh a stale entry through normal fallback; neither warm-up is measured.
	_ = connectAndClose(baseline)
	_ = connectAndClose(optimized)
	if !optimized.eligible {
		t.Skip("Server did not provide a valid database UUID; cookie optimization is unavailable")
	}
	_ = connectAndClose(optimized)
	if !optimized.cacheHit || optimized.renegotiated {
		t.Fatal("cookie warm-up did not complete with an accepted cached cookie")
	}

	// Alternate the order within each pair to reduce bias from network/server drift.
	for i := 0; i < samples; i++ {
		first, second := baseline, optimized
		if i%2 != 0 {
			first, second = second, first
		}
		for _, testCase := range []*connectionTimingCase{first, second} {
			elapsed := connectAndClose(testCase)
			if testCase == optimized && (!testCase.cacheHit || testCase.renegotiated) {
				t.Fatalf("with cookie connection %02d did not use an accepted cached cookie: cache hit=%t, renegotiated=%t",
					i+1, testCase.cacheHit, testCase.renegotiated)
			}
			testCase.total += elapsed
			t.Logf("%s connection %02d: %v", testCase.label, i+1, elapsed)
		}
	}

	baselineAverage := baseline.total / samples
	cookieAverage := optimized.total / samples
	gainPercent := (float64(baselineAverage-cookieAverage) / float64(baselineAverage)) * 100
	t.Logf("without cookie average: %v", baselineAverage)
	t.Logf("with cookie average: %v", cookieAverage)
	t.Logf("cookie optimization gain: %.2f%%", gainPercent)
}

// TestDriver_TTCCookieTTIRENEG exercises server-driven renegotiation on a real
// connection. It requires a server that rejects a stale TTC cookie with TTIRENEG.
func TestDriver_TTCCookieTTIRENEG(t *testing.T) {
	if TestingConfig == nil {
		t.Skip("No configuration available")
	}
	config := NewOracleDriverConfig()
	config.ConnectDescriptor = TestingConfig.GetConnectionString()
	config.DriverProperties.TTCCookieOptimizationEnabled = true
	connector, err := NewOracleConnector(config)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	ttc.ClearTTCCookieCacheForTest()
	t.Cleanup(ttc.ClearTTCCookieCacheForTest)
	trace := observeTTCCookieConnections(t, connector)

	connectAndClose := func() {
		t.Helper()
		db := sql.OpenDB(connector)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			t.Fatalf("connect: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	connectAndClose() // Normal TTC negotiation populates the cache.
	t.Log("Normal TTC negotiation populates the cache.")
	if !trace.eligible {
		t.Skip("Server did not provide a valid database UUID; cookie optimization is unavailable")
	}
	if trace.cacheHit || trace.fallback != nil {
		t.Fatal("first connection did not use normal negotiation")
	}
	uuid := trace.databaseUUID
	originalVersion, ok := ttc.CorruptTTCCookieForTest(uuid)
	if !ok {
		t.Fatal("No TTC cookie was cached by the first connection")
	}
	if originalVersion == 0 {
		t.Fatal("normal negotiation cached an invalid TTC protocol version")
	}

	connectAndClose() // Stale cookie must cause TTIRENEG and normal fallback.
	t.Log("Stale cookie must cause TTIRENEG and normal fallback")
	assertTTCCookieFallback(t, trace)
	t.Log("Observed TTIRENEG; normal negotiation used the server's replacement TTIPRO without a new PRO request")

	version, ok := ttc.TTCCookieProtocolVersionForTest(uuid)
	if !ok || version != originalVersion {
		t.Fatalf("TTIRENEG did not replace the stale cookie: version=%d, found=%t, want=%d", version, ok, originalVersion)
	}

	connectAndClose() // The replacement cookie must work on a later connection.
	if !trace.cacheHit || trace.fallback != nil {
		t.Fatal("replacement cookie was not accepted on the later connection")
	}
	t.Log("The replacement cookie worked on a later connection")
}

// TestDriver_TTCCookieOCIToken exercises cookie acceptance and server-driven
// re-negotiation with a real OCI IAM token connection.
func TestDriver_TTCCookieOCIToken(t *testing.T) {
	connectDescriptor := os.Getenv("ORACLE_GO_OCI_TOKEN_CONNECT_DESCRIPTOR")
	tokenLocation := os.Getenv("ORACLE_GO_OCI_TOKEN_LOCATION")
	expectedUser := os.Getenv("ORACLE_GO_OCI_TOKEN_EXPECTED_USER")
	if connectDescriptor == "" || tokenLocation == "" || expectedUser == "" {
		t.Skip("OCI token test requires ORACLE_GO_OCI_TOKEN_CONNECT_DESCRIPTOR, ORACLE_GO_OCI_TOKEN_LOCATION and ORACLE_GO_OCI_TOKEN_EXPECTED_USER")
	}

	ttc.ClearTTCCookieCacheForTest()
	t.Cleanup(ttc.ClearTTCCookieCacheForTest)

	cfg := NewOracleDriverConfig()
	cfg.ConnectDescriptor = connectDescriptor
	cfg.DriverProperties.TTCCookieOptimizationEnabled = true
	connector, err := NewOracleConnector(cfg)
	if err != nil {
		t.Fatalf("create connector: %v", err)
	}
	registrar, ok := connector.(oracleProviders.ProviderRegistrar)
	if !ok {
		t.Fatal("connector does not support provider registration")
	}
	registrar.RegisterProvider(testOCITokenProvider{
		tokenPath:      filepath.Join(tokenLocation, testOCITokenFileName),
		privateKeyPath: filepath.Join(tokenLocation, testOCIPrivateKeyFileName),
	})
	trace := observeTTCCookieConnections(t, connector)

	connect := func(stage string) {
		t.Helper()
		db := sql.OpenDB(connector)
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("%s: connect: %v", stage, err)
		}
		var user string
		if err := db.QueryRowContext(ctx, "SELECT USER FROM SYS.DUAL").Scan(&user); err != nil {
			t.Fatalf("%s: query current user: %v", stage, err)
		}
		if user != expectedUser {
			t.Fatalf("%s: connected as %q, want %q", stage, user, expectedUser)
		}
		t.Logf("%s: connected as %s", stage, user)
	}

	connect("normal negotiation")
	if !trace.eligible {
		t.Skip("Server did not provide a valid database UUID; cookie optimization is unavailable")
	}
	if trace.cacheHit || trace.fallback != nil {
		t.Fatal("first token connection did not use normal negotiation")
	}
	uuid := trace.databaseUUID
	connect("accepted cookie")
	if trace.databaseUUID != uuid || !trace.cacheHit || trace.fallback != nil {
		t.Fatal("token connection did not use an accepted cached cookie for the same database UUID")
	}
	originalVersion, ok := ttc.CorruptTTCCookieForTest(uuid)
	if !ok || originalVersion == 0 {
		t.Fatalf("normal negotiation did not cache a valid cookie: found=%t, version=%d", ok, originalVersion)
	}
	connect("stale cookie re-negotiation")
	assertTTCCookieFallback(t, trace)
	version, ok := ttc.TTCCookieProtocolVersionForTest(uuid)
	if !ok || version != originalVersion {
		t.Fatalf("re-negotiation did not replace the stale cookie: found=%t, version=%d, want=%d", ok, version, originalVersion)
	}
	connect("replacement cookie")
	if trace.databaseUUID != uuid || !trace.cacheHit || trace.fallback != nil {
		t.Fatal("replacement cookie was not accepted by the later token connection")
	}
}

// Observe real factory selection; no instantiator or server responses are mocked.
type ttcCookieConnectionTrace struct {
	databaseUUID string
	eligible     bool
	cacheHit     bool
	fallback     *ttcCookieFallbackSession
}

func observeTTCCookieConnections(t *testing.T, c driver.Connector) *ttcCookieConnectionTrace {
	t.Helper()
	connector, ok := c.(*connector)
	if !ok {
		t.Fatalf("unexpected connector type: %T", c)
	}
	if !connector.connectorConfig.DriverProperties.TTCCookieOptimizationEnabled {
		t.Fatal("cookie optimization was disabled by external configuration")
	}
	trace := &ttcCookieConnectionTrace{}
	factory := connector.connInstantiatorFactory
	var originalSession driverCommon.NetworkSession
	connector.connInstantiatorFactory = func(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providers common.Registry[oracleProviders.Provider], reneg bool) (driverCommon.ConnectionInstantiator, error) {
		if reneg {
			if ns != originalSession || trace.fallback != nil {
				t.Fatal("TTIRENEG fallback must reuse the same network session exactly once")
			}
			trace.fallback = &ttcCookieFallbackSession{
				NetworkSession: ns,
				DataBuffer:     ns.(driverCommon.DataBuffer),
			}
			return factory(config, trace.fallback, providers, true)
		}

		originalSession = ns
		trace.databaseUUID = ns.GetDatabaseUUID()
		trace.eligible = ttc.IsTTCCookieEligible(config.DriverProperties, trace.databaseUUID)
		trace.cacheHit = false
		trace.fallback = nil
		if trace.eligible {
			_, trace.cacheHit = ttc.GetCachedTTCCookie(trace.databaseUUID)
		}
		return factory(config, ns, providers, false)
	}
	return trace
}

// Observe only the first header read/written during fallback, forwarding every
// byte unchanged to the same real network session.
type ttcCookieFallbackSession struct {
	driverCommon.NetworkSession
	driverCommon.DataBuffer
	firstRead            byte
	firstWrite           byte
	read                 bool
	wrote                bool
	readBeforeFirstWrite bool
}

func (s *ttcCookieFallbackSession) ReadByteWithContext(ctx context.Context) (byte, error) {
	value, err := s.DataBuffer.ReadByteWithContext(ctx)
	if err == nil && !s.read {
		s.firstRead = value
		s.read = true
	}
	return value, err
}

func (s *ttcCookieFallbackSession) WriteByteWithContext(ctx context.Context, value byte) error {
	if !s.wrote {
		s.firstWrite = value
		s.wrote = true
		s.readBeforeFirstWrite = s.read
	}
	return s.DataBuffer.WriteByteWithContext(ctx, value)
}

func assertTTCCookieFallback(t *testing.T, trace *ttcCookieConnectionTrace) {
	t.Helper()
	if !trace.cacheHit || trace.fallback == nil {
		t.Fatal("stale cookie did not cause TTIRENEG and normal fallback")
	}
	fallback := trace.fallback
	if !fallback.read || !fallback.wrote || !fallback.readBeforeFirstWrite ||
		fallback.firstRead != byte(ttc.TTIPRO) || fallback.firstWrite != byte(ttc.TTIDTY) {
		t.Fatalf("fallback must read replacement TTIPRO before sending TTIDTY, without a new client TTIPRO: read=%t, wrote=%t, readBeforeWrite=%t, firstRead=%d, firstWrite=%d",
			fallback.read, fallback.wrote, fallback.readBeforeFirstWrite, fallback.firstRead, fallback.firstWrite)
	}
}
