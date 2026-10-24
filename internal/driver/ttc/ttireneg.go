package ttc

import (
	"context"

	"github.com/oracle/go-oracledb/v26/internal/driver/common"
)

type tTIReneg struct {
}

func newTTIReneg() common.Message[common.MessageType] {
	return &tTIReneg{}
}

func (*tTIReneg) GetMsgCode() common.MessageType { return TTIRENEG }

func (*tTIReneg) UnmarshalFrom(ctx context.Context, mar common.Marshaller) error {
	return nil // TTIRENEG itself has no payload
}
