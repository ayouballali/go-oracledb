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
	"errors"
	"fmt"
	"testing"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	"github.com/oracle/go-oracledb/v26/internal/driver/network/naming"
	oracleconfig "github.com/oracle/go-oracledb/v26/oracle/config"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

// TestConnectorCookieFallback verifies that only TTIRENEG triggers a single
// fallback, the network session is reused, and terminal failures disconnect it.
func TestConnectorCookieFallback(t *testing.T) {
	t.Parallel()
	reneg := fmt.Errorf("optimized connection: %w", driverCommon.ErrTTIRENEG)
	for _, tc := range []struct {
		name        string
		firstErr    error
		fallbackErr error
		factoryErr  error
		wantCalls   int
		wantFailure bool
	}{
		{"successful fallback", reneg, nil, nil, 2, false},
		{"unrelated failure", errors.New("authentication rejected"), nil, nil, 1, true},
		{"repeated RENEG", reneg, driverCommon.ErrTTIRENEG, nil, 2, true},
		{"fallback factory failure", reneg, nil, errors.New("factory failed"), 2, true},
		{"fallback authentication failure", reneg, errors.New("fallback rejected"), nil, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ns := &mockConnectorNetworkSession{}
			networkCalls, factoryCalls := 0, 0
			connector, err := newOracleConnector(newConnectorTestConfig(t), NewOracleDriverConfig(),
				func(context.Context, *naming.ConnectionOption, string) (driverCommon.NetworkSession, error) {
					networkCalls++
					return ns, nil
				},
				func(_ *oracleconfig.OracleDriverConfig, gotNS driverCommon.NetworkSession, _ common.Registry[oracleProviders.Provider], renegotiating bool) (driverCommon.ConnectionInstantiator, error) {
					factoryCalls++
					if gotNS != ns || renegotiating != (factoryCalls == 2) {
						t.Fatal("factory did not receive the same session and expected fallback mode")
					}
					if !renegotiating {
						return mockConnectorConnectionInstantiator{err: tc.firstErr}, nil
					}
					if tc.factoryErr != nil {
						return nil, tc.factoryErr
					}
					if tc.fallbackErr != nil {
						return mockConnectorConnectionInstantiator{err: tc.fallbackErr}, nil
					}
					return mockConnectorConnectionInstantiator{conn: mockConnectorDriverConn{}}, nil
				})
			if err != nil {
				t.Fatalf("newOracleConnector: %v", err)
			}
			conn, err := connector.Connect(context.Background())
			if (err != nil) != tc.wantFailure {
				t.Fatalf("Connect error = %v, want failure %t", err, tc.wantFailure)
			}
			if !tc.wantFailure && conn == nil {
				t.Fatal("successful fallback returned no connection")
			}
			if networkCalls != 1 || factoryCalls != tc.wantCalls {
				t.Fatalf("network/factory calls = %d/%d, want 1/%d", networkCalls, factoryCalls, tc.wantCalls)
			}
			if ns.disconnectCalled != tc.wantFailure {
				t.Fatalf("disconnect = %t, want %t", ns.disconnectCalled, tc.wantFailure)
			}
		})
	}
}
