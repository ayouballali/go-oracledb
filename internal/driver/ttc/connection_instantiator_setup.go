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
type ConnectionInstantiatorSetup struct {
	negotiator          Negotiator
	authenticator       Authenticator
	dataBuffer          driverCommon.DataBuffer
	ns                  driverCommon.NetworkSession
	drvierConfig        *oracleconfig.OracleDriverConfig
	newConnectionFunc   func(context.Context, *ttiShelf[driverCommon.MessageType], *driverCommon.SessionContext, driverCommon.NetworkSession) (*connection, error)
	localizationService common.LocalizationService
	providerRegistry    common.Registry[oracleProviders.Provider]
}

func NewConnectionInstantiatorSetup(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providerRegistry common.Registry[oracleProviders.Provider], negotiatorFactory NegotiatorFactory, authenticatorFactory AuthenticatorFactory) (*ConnectionInstantiatorSetup, error) {
	dataBuffer := ns.(driverCommon.DataBuffer)
	localizationService := common.NewLocalizationService(config.Locale.ClientLanguage)

	negotiator := negotiatorFactory(dataBuffer)

	authenticator, err := authenticatorFactory()
	if err != nil {
		return nil, err
	}

	return &ConnectionInstantiatorSetup{
		dataBuffer:          dataBuffer,
		ns:                  ns,
		drvierConfig:        config,
		newConnectionFunc:   newConnection,
		localizationService: localizationService,
		providerRegistry:    providerRegistry,
		negotiator:          negotiator,
		authenticator:       authenticator,
	}, nil

}

func (setupInstantiator *ConnectionInstantiatorSetup) PrepareForAuthetication(sessCtx *driverCommon.SessionContext, shelf *ttiShelf[driverCommon.MessageType]) {
	// After the session context is created, set client properties that will be needed
	// by the client for authentication
	sessCtx.GetClientProperties().SetProperty(driverCommon.RemoteAddress, setupInstantiator.ns.GetRemoteAddress())
	sessCtx.GetClientProperties().SetProperty(driverCommon.RemotePort, setupInstantiator.ns.GetRemotePort())
	sessCtx.GetClientProperties().SetProperty(driverCommon.ConnectDescriptor, setupInstantiator.drvierConfig.ConnectDescriptor)

	// Add connection properties to shelf for downstream consumers
	shelf.UpdateConnectionProperties(setupInstantiator.drvierConfig.DriverProperties)
	shelf.RegisterLocalizationService(setupInstantiator.localizationService)
	shelf.registerProviderRegistry(setupInstantiator.providerRegistry)

	// register server to client piggyback callbacks
	registerServerToClientPiggybacks(shelf, sessCtx)

	// Initialise authenticator
	//todo remember this are causing a problem of we use the properties before we actually set them
	if authenticator, ok := setupInstantiator.authenticator.(ttiShelfUser); ok {
		authenticator.SetShelf(shelf)
	}
	if sessionContextUser, ok := setupInstantiator.authenticator.(SessionContextUser); ok {
		sessionContextUser.SetSessionContext(sessCtx)
	}
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
func GetAuthenticator(parameters *oracleconfig.OracleDriverConfig, providerRegistry common.Registry[oracleProviders.Provider]) (Authenticator, error) {
	common.Odl.Debug("New authenticator requested for", "parameters", parameters)

	if parameters == nil {
		// this should never happen
		return nil, common.NewOracleError(oracleErrors.InternalError, nil)
	}

	if len(parameters.Credentials.User) > 0 {
		if len(parameters.Credentials.Password) > 0 {
			return createPasswordAuthenticator(parameters)
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
			return newTokenAuthenticator(tokenProvider), nil
		}

	}
	return nil, common.NewOracleError(oracleErrors.NoAuthenticatorError, nil, nil)
}

func createPasswordAuthenticator(parameters *oracleconfig.OracleDriverConfig) (Authenticator, error) {
	if len(parameters.Credentials.LogonMode) > 0 {
		var mode, _ = common.GetLogonModeFromString(parameters.Credentials.LogonMode)
		common.Odl.Debug(fmt.Sprintf("logonMode set to [%s]", mode))
		return newPasswordAuthenticatorWithLogonMode(parameters.Credentials.User,
			parameters.Credentials.Password,
			mode,
			parameters.ConnectDescriptor), nil
	}
	return newPasswordAuthenticator(parameters.Credentials.User,
		parameters.Credentials.Password,
		parameters.ConnectDescriptor), nil
}

// *** Negotiator Factory ***

// GetNegotiator returns the default Negotiator implementation.
// TODO : we should not have to pass the buffer here...
func GetNegotiator(transport driverCommon.DataBuffer) Negotiator {
	n := newConnectionNegotiator(transport)
	return n
}
