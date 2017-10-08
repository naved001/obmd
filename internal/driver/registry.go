package driver

import (
	"encoding/json"
	"errors"
)

// Indicates that Registry.GetOBM was called with a "type" field not in
// the registry.
var ErrUnknownType = errors.New("Unknown obm type")

// A registry is aggregates multiple drivers. It satisfies the driver
// interface itself, expecting it's info to be JSON like:
//
// {"type": someType, "info": driverInfo}
//
// where someType is a JSON string, and driverInfo is arbitrary JSON.
// Its GetOBM method shells out to registry[someType].GetOBM(driverInfo),
// returning ErrUnknownType if someType is not in the registry.
type Registry map[string]Driver

type obmInfo struct {
	Type string      `json:"type"`
	Info *driverInfo `json:"info"`
}

// Wrapper around []byte that lets us collect the raw JSON in obmInfo's Info
// field without parsing it.
type driverInfo []byte

func (info *driverInfo) UnmarshalJSON(data []byte) error {
	*info = driverInfo(make([]byte, len(data)))
	copy(*info, data)
	return nil
}

func (r Registry) GetOBM(info []byte) (OBM, error) {
	obmInfo := obmInfo{
		Info: &driverInfo{},
	}
	err := json.Unmarshal(info, &obmInfo)
	if err != nil {
		return nil, err
	}
	typ, ok := r[obmInfo.Type]
	if !ok {
		return nil, ErrUnknownType
	}
	return typ.GetOBM([]byte(*obmInfo.Info))
}
