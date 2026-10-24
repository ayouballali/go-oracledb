package ttc

import (
	"context"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
)

func readCookieAuthenticationResponse(
	ctx context.Context,
	shelf *ttiShelf[driverCommon.MessageType],
	streamer MessageStreamerInterface,
	funcType driverCommon.FunctionType,
) (driverCommon.Message[driverCommon.MessageType], error) {
	var rpa driverCommon.Message[driverCommon.MessageType] = nil

	createAuthenticationRPA := func(t *messageHeader) (driverCommon.Message[driverCommon.MessageType], error) {
		common.Odl.Fine("Inside oauthRPACallBack hdr", "msg", t.GetType())
		msg, err := shelf.GetMessageFactory().(Factory).GetMessageForFunction(TTIRPA, funcType)
		if err != nil {
			return nil, err
		}
		return msg, nil
	}

	streamer.RegisterPreUnmarshallCallback(TTIRPA, createAuthenticationRPA)
	defer streamer.UnRegisterPreUnmarshallCallback(TTIRPA)

	goOn := true
	for goOn == true {
		msg, err := streamer.Pull(ctx, TTIRPA, TTIOER, TTIWRN, TTIPRO, TTIRENEG, TTIDTY)
		if err != nil {
			common.Odl.Fine("Authenticator failed to pull server messages", "error", err)
			return nil, common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
		}
		switch msg.GetMsgCode() {
		case TTIRPA:
			rpa = msg
			common.Odl.Fine("Authenticator:", "oAuth-RPA", rpa)
		case TTIOER:
			ttioer := msg.(tTIOerIface)
			err := ttioer.getError()
			if err != nil {
				return nil, err
			}
			common.Odl.Fine("Authenticator: TTIOER message received", "ErrorMessage", ttioer)
			goOn = false
		case TTIWRN:
			logAuthenticationWarning(msg.(*tTIwrn))
		case TTIDTY:
		case TTIPRO:
		case TTIRENEG:
			return nil, driverCommon.ErrTTIRENEG
		default:
			common.Odl.Warn("Unexpected message type during oauthMsg", "message", msg)
			return nil, common.NewOracleError(oracleErrors.InternalError, err, nil)
		}
	}

	if err := shelf.checkCurrentState(ctx); err != nil {
		return nil, err
	}

	if rpa == nil {
		// we did not receive any RPA. defend ourselves against avoid malicious
		// attack that would send us an OER but no RPA.
		common.Odl.Warn("Did not received expected RPA yet")
		return nil, common.NewOracleError(oracleErrors.InternalError, nil, nil)
	}

	common.Odl.Fine("Authenticator: processing RPA")
	return rpa, nil
}
