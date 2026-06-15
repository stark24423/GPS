package pairrecord

import (
	"fmt"
	"os"
	"path/filepath"

	"gpssim/internal/ioscore/plist"
)

type PairRecord struct {
	HostID          string
	SystemBUID      string
	HostCertificate []byte
	HostPrivateKey  []byte
}

func Read(udid string) (PairRecord, error) {
	if udid == "" {
		return PairRecord{}, fmt.Errorf("UDID is required")
	}
	path := filepath.Join(`C:\ProgramData\Apple\Lockdown`, udid+".plist")
	data, err := os.ReadFile(path)
	if err != nil {
		return PairRecord{}, fmt.Errorf("read pair record %s: %w", path, err)
	}
	dict, err := plist.Unmarshal(data)
	if err != nil {
		return PairRecord{}, fmt.Errorf("parse pair record: %w", err)
	}
	record := PairRecord{
		HostID:          plist.String(dict, "HostID"),
		SystemBUID:      plist.String(dict, "SystemBUID"),
		HostCertificate: plist.Data(dict, "HostCertificate"),
		HostPrivateKey:  plist.Data(dict, "HostPrivateKey"),
	}
	if record.HostID == "" || record.SystemBUID == "" {
		return PairRecord{}, fmt.Errorf("pair record missing HostID/SystemBUID")
	}
	if len(record.HostCertificate) == 0 || len(record.HostPrivateKey) == 0 {
		return PairRecord{}, fmt.Errorf("pair record missing host TLS certificate/key")
	}
	return record, nil
}
