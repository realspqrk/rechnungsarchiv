//go:build !windows

package creds

// system has no backend outside Windows (v1 ships a Windows GUI only).
type system struct{}

func (system) Set(string, string, Credential) error   { return ErrUnsupported }
func (system) Get(string, string) (Credential, error) { return Credential{}, ErrUnsupported }
func (system) Has(string, string) (bool, error)       { return false, ErrUnsupported }
func (system) Delete(string, string) error            { return ErrUnsupported }
