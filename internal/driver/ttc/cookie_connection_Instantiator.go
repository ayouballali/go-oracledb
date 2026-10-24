package ttc

import (
	"context"
	"database/sql/driver"

	"github.com/oracle/go-oracledb/v26/internal/common"
	driverCommon "github.com/oracle/go-oracledb/v26/internal/driver/common"
	oracleconfig "github.com/oracle/go-oracledb/v26/oracle/config"
	oracleErrors "github.com/oracle/go-oracledb/v26/oracle/errors"
	oracleProviders "github.com/oracle/go-oracledb/v26/oracle/providers"
)

type cookieConnectionInstantiator struct {
	instantiatorSetup *ConnectionInstantiatorSetup
	cookie            *ttiCookie
}

func NewTTCOptimizedConnectionInstantiator(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providerRegistry common.Registry[oracleProviders.Provider], cookie *ttiCookie) (*cookieConnectionInstantiator, error) {

	createNegotiator := func(buffer driverCommon.DataBuffer) Negotiator {
		return GetNegotiator(
			buffer,
			driverCommon.ConnectionCookie,
			cookie,
		)
	}

	createAuthenticator := func() (Authenticator, error) {
		return GetAuthenticator(
			config,
			providerRegistry,
			driverCommon.ConnectionCookie,
		)
	}

	instantiatorSetup, err := NewConnectionInstantiatorSetup(config, ns, providerRegistry, createNegotiator, createAuthenticator)
	if err != nil {
		return nil, err
	}

	return &cookieConnectionInstantiator{
		instantiatorSetup: instantiatorSetup,
	}, nil
}

func (optimizedConnInstantiator *cookieConnectionInstantiator) GetConnection(ctx context.Context) (driver.Conn, error) {
	setup := optimizedConnInstantiator.instantiatorSetup
	negotiator := optimizedConnInstantiator.instantiatorSetup.negotiator
	authenticator := optimizedConnInstantiator.instantiatorSetup.authenticator
	if setup.localizationService == nil {
		return nil, common.NewOracleError(oracleErrors.MissingLocalizationService, nil)
	}

	// Perform wire negotiation; capture session context, shelf and capability
	sessCtx, shelf, _, err := negotiator.Negotiate(ctx)
	if err != nil {
		return nil, setup.localizationService.LocalizeError(err)
	}

	streamer := shelf.GetMessageStreamer().(MessageStreamerInterface)
	defer unregisterCookieNegotiationCallbacks(streamer)

	setup.PrepareForAuthetication(sessCtx, shelf)

	// Authenticate
	err = authenticator.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	// snapshot session
	sessCtx.GetSessionProperties().Snapshot()

	common.Odl.Debug("Connect end")
	return setup.newConnectionFunc(ctx, shelf, sessCtx, setup.ns)
}
