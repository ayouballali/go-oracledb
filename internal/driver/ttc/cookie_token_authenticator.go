package ttc

import (
	"context"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

type cookieTokenAuthenticator struct {
	tokenSetupAuthenticator *tokenAuthenticatorSetup
}

func newCookieTokenAuthenticator(tokenProvider oracleProviders.TokenAuthenticationProvider) *cookieTokenAuthenticator {
	authSetup := &tokenAuthenticatorSetup{
		tokenProvider: tokenProvider,
	}
	return &cookieTokenAuthenticator{
		tokenSetupAuthenticator: authSetup,
	}
}

func (ta *cookieTokenAuthenticator) Authenticate(ctx context.Context) error {
	setup := ta.tokenSetupAuthenticator

	common.Odl.Debug("Start TOKEN authentication")

	msg, err := setup.prepareOAuth(ctx)
	if err != nil {
		return err
	}

	shelf := setup.shelf
	streamer := shelf.GetMessageStreamer().(MessageStreamerInterface)
	// send oauth message
	if err := streamer.Push(ctx, msg); err != nil {
		return common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
	}
	if err := streamer.Flush(ctx); err != nil {
		return common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
	}
	response, err := readCookieAuthenticationResponse(ctx, &shelf, streamer, oauth)
	if err != nil {
		return err
	}
	oauthrpa := response.(*OAuthRPA)
	setup.sessionContext.UpdateSessionProperties(oauthrpa.connectionValues)

	return nil
}

// SetShelf stores the TTC shelf used to create and exchange authentication
// messages.
//
// Parameters:
//   - shelf: the TTC shelf bound to the current connection attempt.
func (ta *cookieTokenAuthenticator) SetShelf(shelf *ttiShelf[driverCommon.MessageType]) {
	ta.tokenSetupAuthenticator.shelf = *shelf
}

// SetSessionContext stores the session context used during token
// authentication.
//
// Parameters:
//   - sessCtx: the session context associated with the current connection attempt.
func (ta *cookieTokenAuthenticator) SetSessionContext(sessCtx *driverCommon.SessionContext) {
	ta.tokenSetupAuthenticator.sessionContext = sessCtx
}
