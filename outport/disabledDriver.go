package outport

import (
	"github.com/ElrondNetwork/elrond-go/data"
)

var _ Driver = (*disabledOutportDriver)(nil)

type disabledOutportDriver struct {
}

// NewDisabledOutportDriver creates a disabled outport driver
func NewDisabledOutportDriver() *disabledOutportDriver {
	return &disabledOutportDriver{}
}

// DigestBlock does nothing
func (driver *disabledOutportDriver) DigestCommittedBlock(header data.HeaderHandler, body data.BodyHandler) {
}

// IsInterfaceNil returns true if there is no value under the interface
func (driver *disabledOutportDriver) IsInterfaceNil() bool {
	return driver == nil
}