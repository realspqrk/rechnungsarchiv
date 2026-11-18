package creds

import (
	"encoding/json"
	"errors"

	"github.com/danieljoos/wincred"
)

// system stores each credential as a generic Windows credential. The blob
// (protected by the Credential Manager) holds the JSON of the Credential.
type system struct{}

func (system) Set(archiveID, provider string, c Credential) error {
	k, err := target(archiveID, provider)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(c)
	if err != nil {
		return err
	}
	g := wincred.NewGenericCredential(k)
	g.UserName = c.User
	g.Comment = "Rechnungsarchiv: Zugang zum Zeitstempel-Anbieter"
	g.CredentialBlob = blob
	g.Persist = wincred.PersistLocalMachine
	return g.Write()
}

func (system) Get(archiveID, provider string) (Credential, error) {
	k, err := target(archiveID, provider)
	if err != nil {
		return Credential{}, err
	}
	g, err := wincred.GetGenericCredential(k)
	if errors.Is(err, wincred.ErrElementNotFound) {
		return Credential{}, ErrNotFound
	}
	if err != nil {
		return Credential{}, err
	}
	var c Credential
	if err := json.Unmarshal(g.CredentialBlob, &c); err != nil {
		return Credential{}, errors.New("gespeicherte Zugangsdaten sind beschädigt")
	}
	return c, nil
}

func (s system) Has(archiveID, provider string) (bool, error) {
	_, err := s.Get(archiveID, provider)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (system) Delete(archiveID, provider string) error {
	k, err := target(archiveID, provider)
	if err != nil {
		return err
	}
	g, err := wincred.GetGenericCredential(k)
	if errors.Is(err, wincred.ErrElementNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return g.Delete()
}
