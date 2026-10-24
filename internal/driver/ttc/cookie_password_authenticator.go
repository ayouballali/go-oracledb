package ttc

import (
	"context"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
)

type cookiePasswordAuthenticator struct {
	setup *passwordAuthenticatorSetup
}

func newCookiePasswordAuthenticator(username, password string, connectString string) *cookiePasswordAuthenticator {
	username, proxyClient := parseUsername(username)
	setup := &passwordAuthenticatorSetup{
		_connectString: connectString,
		_username:      driverCommon.StringToB1Array(username),
		_proxyClient:   driverCommon.StringToB1Array(proxyClient),
		_password:      driverCommon.StringToB1Array(sanitizeInputCredential(password)),
		_logonMode:     0,
	}

	return &cookiePasswordAuthenticator{
		setup: setup,
	}
}

// newPasswordAuthenticatorWithLogonMode instantiates a new password authenticator.
// Parameters:
//   - username : the username
//   - password : user password
//   - connectString : connectionString
//   - mode : logonMode
func newCookiePasswordAuthenticatorWithLogonMode(username, password string, mode common.LogonMode, connectString string) *cookiePasswordAuthenticator {
	pa := newCookiePasswordAuthenticator(username, password, connectString)
	pa.setup._logonMode = mode.Value()
	return pa
}

func (pa *cookiePasswordAuthenticator) Authenticate(ctx context.Context) error {
	setup := pa.setup
	common.Odl.Fine("Authenticate start for user", "userName", setup._username)
	shelf := setup._shelf

	streamer := shelf.GetMessageStreamer().(MessageStreamerInterface)

	if err := setup.validatePasswordLength(); err != nil {
		return err
	}

	osesskey, err := setup.prepareOSESSKEY()
	if err != nil {
		return err
	}

	err = streamer.Push(ctx, osesskey)
	if err != nil {
		common.Odl.Warn("Failed to push osesskey message to streamer", "error", err)
		return common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
	}

	err = streamer.Flush(ctx)
	if err != nil {
		common.Odl.Warn("failed to flush streamer", "error", err)
		return common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
	}

	response, err := readCookieAuthenticationResponse(ctx, &shelf, streamer, oSesskey)
	if err != nil {
		return err
	}

	osesskeyrpa := response.(*oSesskeyRPA)
	setup._sessionContext.UpdateSessionProperties(osesskeyrpa.connectionValues)

	err = setup._doOAuth(ctx)
	if err != nil {
		common.Odl.Warn("Authentication failed, oauth keys exchange failed", "error", err)
		return common.NewOracleError(oracleErrors.AuthenticatorError, err, nil)
	}
	return nil
}

// SetSessionContext sets the session context to use within the authenticator.
func (pa *cookiePasswordAuthenticator) SetSessionContext(sessCtx *driverCommon.SessionContext) {
	pa.setup._sessionContext = sessCtx
}

// SetShelf sets the shelf to use within the authenticator.
func (pa *cookiePasswordAuthenticator) SetShelf(shelf *ttiShelf[driverCommon.MessageType]) {
	pa.setup._shelf = *shelf
}
