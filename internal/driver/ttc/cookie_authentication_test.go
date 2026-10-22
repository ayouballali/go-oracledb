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
	"errors"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	"io"
	"testing"
)

type cookieResponseTestStreamer struct {
	*MessageStreamer
	responses []driverCommon.Message[driverCommon.MessageType]
}

func (s *cookieResponseTestStreamer) Pull(context.Context, ...driverCommon.MessageType) (driverCommon.Message[driverCommon.MessageType], error) {
	if len(s.responses) == 0 {
		return nil, io.EOF
	}
	msg := s.responses[0]
	s.responses = s.responses[1:]
	return msg, nil
}

// TestCookieAuthenticationResponses verifies password/token RPA preservation,
// delayed negotiation messages, server errors, RENEG signaling, and RPA cleanup.
func TestCookieAuthenticationResponses(t *testing.T) {
	t.Parallel()
	passwordRPA := &oSesskeyRPA{}
	tokenRPA := &OAuthRPA{}
	serverErr := errors.New("authentication rejected")
	for _, tc := range []struct {
		name        string
		function    driverCommon.FunctionType
		responses   []driverCommon.Message[driverCommon.MessageType]
		want        driverCommon.Message[driverCommon.MessageType]
		wantErr     error
		wantFailure bool
	}{
		{"password", oSesskey, []driverCommon.Message[driverCommon.MessageType]{passwordRPA, &mockOer{}}, passwordRPA, nil, false},
		{"token with delayed negotiation", oauth, []driverCommon.Message[driverCommon.MessageType]{newTTIpro(), NewTTIdty(), tokenRPA, &mockOer{}}, tokenRPA, nil, false},
		{"renegotiation", oauth, []driverCommon.Message[driverCommon.MessageType]{newTTIReneg()}, nil, driverCommon.ErrTTIRENEG, true},
		{"server error", oauth, []driverCommon.Message[driverCommon.MessageType]{&mockOer{err: serverErr}}, nil, serverErr, true},
		{"missing RPA", oauth, []driverCommon.Message[driverCommon.MessageType]{&mockOer{}}, nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shelf := newShelf[driverCommon.MessageType]()
			streamer := &cookieResponseTestStreamer{MessageStreamer: NewMessageStreamer(shelf), responses: tc.responses}
			got, err := readCookieAuthenticationResponse(context.Background(), shelf, streamer, tc.function)
			if (err != nil) != tc.wantFailure {
				t.Fatalf("reader error = %v, want failure %t", err, tc.wantFailure)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("reader error = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("reader RPA = %T, want %T", got, tc.want)
			}
			if streamer.preUCallbacks[TTIRPA] != nil {
				t.Fatal("RPA callback leaked after the reader returned")
			}
		})
	}
}
