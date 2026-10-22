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

package ttc

import (
	"context"
	"testing"

	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
)

// TestPasswordAuthenticator_doOAuth_Golden
// 1) Build an oSesskeyRPA by unmarshalling the golden dump.
// 2) Seed the incoming wire with TTIRPA + golden oAuth RPA payload.
// 3) Run _doOAuth to assert marshalling/unmarshalling works end-to-end.
func TestPasswordAuthenticator_doOAuth_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Build oSesskeyRPA input for _doOAuth from the golden payload.
	osrpaMsg := NewOSesskeyRPA()
	rpaBuf := NewArrayDataBuffer(8192)
	if err := rpaBuf.WriteBytesWithContext(ctx, makeOSesskeyRPAPayload()); err != nil {
		t.Fatalf("write oSesskeyRPA payload to temp buffer failed: %v", err)
	}
	rpaEngine := NewMarshalEngine(rpaBuf, driverCommon.BIG_ENDIAN, [5]byte{Native, Universal, Universal, Universal, Universal})
	unmarshallable, _ := osrpaMsg.(driverCommon.Unmarshallable)
	if err := unmarshallable.UnmarshalFrom(ctx, rpaEngine); err != nil {
		t.Fatalf("UnmarshalFrom(oSesskeyRPA) failed: %v", err)
	}

	// Wire up authenticator shelf for oAuth.
	shelf, _, buf := newAuthTestShelf(1 << 16)

	// Pre-seed incoming wire with: [TTIWRN][TTIRPA][golden oAuth RPA payload]
	writeAuthWarning(t, ctx, buf, 28002, "ORA-28002: the password will expire within 7 days", 0x05)
	if err := buf.WriteByteWithContext(ctx, byte(TTIRPA)); err != nil {
		t.Fatalf("write TTIRPA header failed: %v", err)
	}
	if err := buf.WriteBytesWithContext(ctx, makeOauthRPAPayload()); err != nil {
		t.Fatalf("write oAuth RPA payload failed: %v", err)
	}

	pa := newPasswordAuthenticator(
		"username_test",
		"username_test",
		"(DESCRIPTION=(ADDRESS=(HOST=localhost)(PORT=1521)(PROTOCOL=tcp))(CONNECT_DATA=(SERVICE_NAME=freepdb1)))")
	pa.SetShelf(shelf)
	sessCtx := driverCommon.NewSessionContext()
	pa.SetSessionContext(sessCtx)
	sessCtx.UpdateSessionProperties(osrpaMsg.(*oSesskeyRPA).connectionValues)
	if err := pa.setup._doOAuth(ctx); err != nil {
		sqlError, ok := err.(oracleErrors.SQLError)
		if !ok {
			t.Fatalf("expected error to be SQLError")
		}
		if sqlError.ErrorCode() != string(oracleErrors.AuthenticatorError) {
			t.Fatalf("Expected error %s, but got %s", oracleErrors.AuthenticatorError, sqlError.ErrorCode())
		}
	}
}
