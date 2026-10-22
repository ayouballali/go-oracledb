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
	"testing"
)

// TestCookieNegotiationQueuesMessages verifies that cookie negotiation prepares
// PRO, COOKIE, and DTY in order without flushing before authentication.
func TestCookieNegotiationQueuesMessages(t *testing.T) {
	t.Parallel()
	pro := newTTIpro().(*tTIpro)
	pro.proSvrVer, pro.svrCharSet, pro.nCharCharset = 6, 873, 2000
	cookie := newTTICookieFromTTIpro(pro)
	buffer := NewArrayDataBuffer(4096)
	negotiator := newCookieConnectionNegotiator(buffer, cookie)
	sessCtx, shelf, returnedCookie, err := negotiator.Negotiate(context.Background())
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if sessCtx == nil || shelf == nil || returnedCookie != nil {
		t.Fatal("unexpected cookie negotiation result")
	}
	streamer := shelf.GetMessageStreamer().(*MessageStreamer)
	if buffer.currentWritePosition != 0 || buffer.hasBeenFlushed {
		t.Fatal("negotiation wrote or flushed before authentication")
	}
	want := []driverCommon.MessageType{TTIPRO, TTICOOKIE, TTIDTY}
	if streamer.outgoingMessages.Len() != len(want) {
		t.Fatalf("queued messages = %d, want %d", streamer.outgoingMessages.Len(), len(want))
	}
	i := 0
	for entry := streamer.outgoingMessages.Front(); entry != nil; entry = entry.Next() {
		if got := entry.Value.(driverCommon.Message[driverCommon.MessageType]).GetMsgCode(); got != want[i] {
			t.Fatalf("queued message %d = %v, want %v", i, got, want[i])
		}
		i++
	}
	decodeErr := errors.New("datatype decode failed")
	if _, err := streamer.postUCallbacks[TTIDTY](NewTTIdty(), decodeErr); err != decodeErr {
		t.Fatalf("DTY callback error = %v, want original decode error", err)
	}
	streamer.RegisterPreUnmarshallCallback(TTIRPA, func(*messageHeader) (driverCommon.Message[driverCommon.MessageType], error) { return nil, nil })
	unregisterCookieNegotiationCallbacks(streamer)
	if streamer.preUCallbacks[TTIDTY] != nil || streamer.postUCallbacks[TTIDTY] != nil {
		t.Fatal("cookie DTY callbacks were not removed")
	}
	if streamer.preUCallbacks[TTIRPA] == nil {
		t.Fatal("cookie cleanup removed an unrelated authentication callback")
	}
}
