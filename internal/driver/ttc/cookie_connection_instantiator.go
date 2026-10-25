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
	shared *ConnectionInstantiatorCommon
}

// NewTTCCookieConnectionInstantiator binds the cached cookie to the existing network session.
// It returns an error if the authentication dependencies cannot be created.
func NewTTCCookieConnectionInstantiator(config *oracleconfig.OracleDriverConfig, ns driverCommon.NetworkSession, providerRegistry common.Registry[oracleProviders.Provider], cookie *ttiCookie) (*cookieConnectionInstantiator, error) {

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

	shared, err := NewConnectionInstantiatorCommon(config, ns, providerRegistry, createNegotiator, createAuthenticator)
	if err != nil {
		return nil, err
	}

	return &cookieConnectionInstantiator{
		shared: shared,
	}, nil
}

// GetConnection completes cookie negotiation and authentication, returning the connection
// or an error. ErrTTIRENEG tells the connector to fall back to normal negotiation.
func (ci *cookieConnectionInstantiator) GetConnection(ctx context.Context) (driver.Conn, error) {
	shared := ci.shared
	negotiator := ci.shared.negotiator
	authenticator := ci.shared.authenticator
	if shared.localizationService == nil {
		return nil, common.NewOracleError(oracleErrors.MissingLocalizationService, nil)
	}

	// Queue cookie negotiation messages and prepare the cached session settings.
	// The authenticator sends and reads the combined exchange.
	sessCtx, shelf, _, err := negotiator.Negotiate(ctx)
	if err != nil {
		return nil, shared.localizationService.LocalizeError(err)
	}

	streamer := shelf.GetMessageStreamer().(MessageStreamerInterface)
	// Negotiation callbacks must remain available until authentication finishes,
	// including when authentication returns an error or requests fallback.
	defer unregisterCookieNegotiationCallbacks(streamer)

	shared.PrepareForAuthentication(sessCtx, shelf)

	// Authenticate
	err = authenticator.Authenticate(ctx)
	if err != nil {
		return nil, err
	}
	// snapshot session
	sessCtx.GetSessionProperties().Snapshot()

	common.Odl.Debug("Connect end")
	return shared.newConnectionFunc(ctx, shelf, sessCtx, shared.ns)
}
