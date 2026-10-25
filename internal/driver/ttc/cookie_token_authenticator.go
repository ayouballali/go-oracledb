package ttc

import (
	"context"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

type cookieTokenAuthenticator struct {
	shared *tokenAuthenticatorCommon
}

// newCookieTokenAuthenticator prepares the cookie path using the supplied token provider.
func newCookieTokenAuthenticator(tokenProvider oracleProviders.TokenAuthenticationProvider) *cookieTokenAuthenticator {
	shared := &tokenAuthenticatorCommon{
		tokenProvider: tokenProvider,
	}
	return &cookieTokenAuthenticator{
		shared: shared,
	}
}

// Authenticate combines cookie negotiation with token OAUTH.
// It returns authentication errors or ErrTTIRENEG when normal fallback is required.
func (ta *cookieTokenAuthenticator) Authenticate(ctx context.Context) error {
	shared := ta.shared

	common.Odl.Debug("Start TOKEN authentication")

	msg, err := shared.prepareOAuth(ctx)
	if err != nil {
		return err
	}

	shelf := shared.shelf
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
	shared.sessionContext.UpdateSessionProperties(oauthrpa.connectionValues)

	return nil
}

// SetShelf stores the TTC shelf used to create and exchange authentication
// messages.
//
// Parameters:
//   - shelf: the TTC shelf bound to the current connection attempt.
func (ta *cookieTokenAuthenticator) SetShelf(shelf *ttiShelf[driverCommon.MessageType]) {
	ta.shared.shelf = *shelf
}

// SetSessionContext stores the session context used during token
// authentication.
//
// Parameters:
//   - sessCtx: the session context associated with the current connection attempt.
func (ta *cookieTokenAuthenticator) SetSessionContext(sessCtx *driverCommon.SessionContext) {
	ta.shared.sessionContext = sessCtx
}
