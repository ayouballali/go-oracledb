package ttc

import (
	"context"

	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
)

type cookieConnectionNegotiator struct {
	negotiationSetup *negotiationSetup
	cookie           *ttiCookie
}

func newCookieConnectionNegotiator(buf driverCommon.DataBuffer, cookie *ttiCookie) *cookieConnectionNegotiator {
	negotiation := &negotiationSetup{
		_dataBuffer: buf,
	}
	return &cookieConnectionNegotiator{
		negotiationSetup: negotiation,
		cookie:           cookie,
	}
}

func (c cookieConnectionNegotiator) Negotiate(ctx context.Context) (*driverCommon.SessionContext, *ttiShelf[driverCommon.MessageType], *ttiCookie, error) {
	negotiateSetup := c.negotiationSetup
	shelf, sessCtx, msgStmr, _ := negotiateSetup.Init()
	pro := c.cookie.newPRO()
	// Register negotiated server capabilities
	shelf.RegisterCapabilities(pro.serverCaps.toMap())
	if !isCharSetMultibyte(pro.svrCharSet) {
		typeRepresentationTable.SetFlags(TTCLXMCONV)
	}

	dty := NewTTIdty().(*tTIdty)
	dty.SetNegotiatedCapabilities(pro.clientCaps)

	err := msgStmr.Push(ctx, pro)
	if err != nil {
		return nil, nil, nil, err
	}

	err = msgStmr.Push(ctx, c.cookie)
	if err != nil {
		return nil, nil, nil, err
	}

	err = msgStmr.Push(ctx, dty)
	if err != nil {
		return nil, nil, nil, err
	}

	negotiateSetup.configureNegotiation(shelf, sessCtx, pro, dty)

	dtyPreCallBack := func(header *messageHeader) (driverCommon.Message[driverCommon.MessageType], error) {
		msg, err := shelf.GetMessageFactory().(Factory).GetMessage(TTIDTY)
		if err != nil {
			return nil, err
		}
		msg.(*tTIdty).SetNegotiatedCapabilities(pro.clientCaps)
		return msg, nil
	}
	dtyPostCallBack := func(msg driverCommon.Message[driverCommon.MessageType], prevErr error) (bool, error) {
		if prevErr != nil {
			return true, prevErr
		}
		dty := msg.(*tTIdty)
		sessCtx.SetTimeZoneVersionNumber(dty.GetTimeZoneVersionNumber())

		return true, nil
	}
	msgStmr.RegisterPostUnmarshallCallback(TTIDTY, dtyPostCallBack)
	msgStmr.RegisterPreUnmarshallCallback(TTIDTY, dtyPreCallBack)

	return sessCtx, shelf, nil, nil
}

func unregisterCookieNegotiationCallbacks(
	msgStmr MessageStreamerInterface,
) {
	msgStmr.UnRegisterPreUnmarshallCallback(TTIDTY)
	msgStmr.UnRegisterPostUnmarshallCallback(TTIDTY)
}
