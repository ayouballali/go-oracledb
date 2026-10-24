package ttc

import (
	"context"
	"fmt"
	"reflect"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleconfig "github.com/oracle/go-oracledb/v26/oracle/config"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

type NegotiatorFactory func(driverCommon.DataBuffer) Negotiator
type AuthenticatorFactory func() (Authenticator, error)

// ConnectionInstantiatorCommon holds per-connection dependencies and preparation
// shared by the normal and cookie connection instantiators.
type ConnectionInstantiatorCommon struct {
	negotiator          Negotiator
	authenticator       Authenticator
	dataBuffer          driverCommon.DataBuffer
	ns                  driverCommon.NetworkSession
	driverConfig        *oracleconfig.OracleDriverConfig
	newConnectionFunc   func(context.Context, *ttiShelf[driverCommon.MessageType], *driverCommon.SessionContext, driverCommon.NetworkSession) (*connection, error)
	localizationService common.LocalizationService
	providerRegistry    common.Registry[oracleProviders.Provider]
}

// NewConnectionInstantiatorCommon creates the dependencies shared by connection-establishment paths.
func NewConnectionInstantiatorCommon(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providerRegistry common.Registry[oracleProviders.Provider], negotiatorFactory NegotiatorFactory, authenticatorFactory AuthenticatorFactory) (*ConnectionInstantiatorCommon, error) {
	dataBuffer := ns.(driverCommon.DataBuffer)
	localizationService := common.NewLocalizationService(config.Locale.ClientLanguage)

	negotiator := negotiatorFactory(dataBuffer)

	authenticator, err := authenticatorFactory()
	if err != nil {
		return nil, err
	}

	return &ConnectionInstantiatorCommon{
		dataBuffer:          dataBuffer,
		ns:                  ns,
		driverConfig:        config,
		newConnectionFunc:   newConnection,
		localizationService: localizationService,
		providerRegistry:    providerRegistry,
		negotiator:          negotiator,
		authenticator:       authenticator,
	}, nil

}

// PrepareForAuthentication installs connection properties, callbacks, and authenticator dependencies.
func (shared *ConnectionInstantiatorCommon) PrepareForAuthentication(sessCtx *driverCommon.SessionContext, shelf *ttiShelf[driverCommon.MessageType]) {
	// After the session context is created, set client properties that will be needed
	// by the client for authentication
	sessCtx.GetClientProperties().SetProperty(driverCommon.RemoteAddress, shared.ns.GetRemoteAddress())
	sessCtx.GetClientProperties().SetProperty(driverCommon.RemotePort, shared.ns.GetRemotePort())
	sessCtx.GetClientProperties().SetProperty(driverCommon.ConnectDescriptor, shared.driverConfig.ConnectDescriptor)

	// Add connection properties to shelf for downstream consumers
	shelf.UpdateConnectionProperties(shared.driverConfig.DriverProperties)
	shelf.RegisterLocalizationService(shared.localizationService)
	shelf.registerProviderRegistry(shared.providerRegistry)

	// register server to client piggyback callbacks
	registerServerToClientPiggybacks(shelf, sessCtx)

	// Initialise authenticator
	//todo remember this are causing a problem of we use the properties before we actually set them
	if authenticator, ok := shared.authenticator.(ttiShelfUser); ok {
		authenticator.SetShelf(shelf)
	}
	if sessionContextUser, ok := shared.authenticator.(SessionContextUser); ok {
		sessionContextUser.SetSessionContext(sessCtx)
	}
}

// *** Negotiator Factory ***

// GetNegotiator selects normal, cookie, or server-requested re-negotiation.
// TODO : we should not have to pass the buffer here...
func GetNegotiator(dataBuffer driverCommon.DataBuffer, mode driverCommon.ConnectionMode, cookie *ttiCookie) Negotiator {
	var n Negotiator
	switch mode {
	case driverCommon.ConnectionCookie:
		n = newCookieConnectionNegotiator(dataBuffer, cookie)
	case driverCommon.ConnectionNormal:
		n = newConnectionNegotiator(dataBuffer, false)
	case driverCommon.ConnectionRenegotiate:
		n = newConnectionNegotiator(dataBuffer, true)
	}

	return n
}

// *** Authenticator Factory ***

// GetAuthenticator selects the authenticator to use for the supplied driver
// configuration and provider registry.
//
// Parameters:
//   - parameters: the Oracle driver configuration to inspect.
//   - providerRegistry: the provider registry available to token-based authentication.
//
// Returns:
//   - the authenticator matching the supplied configuration.
//   - an error if no suitable authenticator can be created.
func GetAuthenticator(parameters *oracleconfig.OracleDriverConfig, providerRegistry common.Registry[oracleProviders.Provider], mode driverCommon.ConnectionMode) (Authenticator, error) {
	common.Odl.Debug("New authenticator requested for", "parameters", parameters)

	if parameters == nil {
		// this should never happen
		return nil, common.NewOracleError(oracleErrors.InternalError, nil)
	}

	if len(parameters.Credentials.User) > 0 {
		if len(parameters.Credentials.Password) > 0 {

			return createPasswordAuthenticator(parameters, mode)
		}
	} else {
		if len(parameters.Credentials.Password) > 0 {
			// if there is a password but no username
			return nil, common.NewOracleError(oracleErrors.EmptyUsernameError, nil, nil)
		}
		provider, err := providerRegistry.Get(reflect.TypeOf((*oracleProviders.TokenAuthenticationProvider)(nil)).Elem())
		if err != nil {
			return nil, common.NewOracleError(oracleErrors.NoAuthenticatorError, err, nil)
		}
		if provider != nil {
			tokenProvider := provider.(oracleProviders.TokenAuthenticationProvider)
			switch mode {
			case driverCommon.ConnectionCookie:
				return newCookieTokenAuthenticator(tokenProvider), nil
			case driverCommon.ConnectionNormal, driverCommon.ConnectionRenegotiate:
				return newTokenAuthenticator(tokenProvider), nil
			}
		}

	}
	return nil, common.NewOracleError(oracleErrors.NoAuthenticatorError, nil, nil)
}

func createPasswordAuthenticator(parameters *oracleconfig.OracleDriverConfig, connectionMode driverCommon.ConnectionMode) (Authenticator, error) {
	hasLogonMode := len(parameters.Credentials.LogonMode) > 0
	var logonMode common.LogonMode
	if hasLogonMode {
		var err error
		logonMode, err = common.GetLogonModeFromString(parameters.Credentials.LogonMode)
		if err != nil {
			return nil, err
		}
		common.Odl.Debug(fmt.Sprintf("logonMode set to [%s]", logonMode))
	}

	switch connectionMode {
	case driverCommon.ConnectionCookie:
		if hasLogonMode {
			return newCookiePasswordAuthenticatorWithLogonMode(parameters.Credentials.User,
				parameters.Credentials.Password,
				logonMode,
				parameters.ConnectDescriptor), nil
		}
		return newCookiePasswordAuthenticator(parameters.Credentials.User,
			parameters.Credentials.Password,
			parameters.ConnectDescriptor), nil
	case driverCommon.ConnectionNormal, driverCommon.ConnectionRenegotiate:
		if hasLogonMode {
			return newPasswordAuthenticatorWithLogonMode(parameters.Credentials.User,
				parameters.Credentials.Password,
				logonMode,
				parameters.ConnectDescriptor), nil
		}
		return newPasswordAuthenticator(parameters.Credentials.User,
			parameters.Credentials.Password,
			parameters.ConnectDescriptor), nil
	default:
		return nil, common.NewOracleError(oracleErrors.InternalError, nil)
	}
}
