package state

import (
	"encoding/json"
	"os"
	"sync"
)

// State is state.json
type State struct {
	AgentUUID string `json:"agent_uuid"`
	Password  string `json:"password"`

	l    sync.Mutex
	path string
}

// Load load state.json file
func Load(path string) (*State, error) {
	state := State{
		path: path,
	}
	f, err := os.Open(path)
	if err != nil && os.IsNotExist(err) {
		return &state, nil
	} else if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&state)
	return &state, err
}

// Save will write back the State to state.json
func (s *State) Save() error {
	s.l.Lock()
	defer s.l.Unlock()
	err := s.saveTo(s.path + ".tmp")
	if err != nil {
		return nil
	}
	err = os.Rename(s.path+".tmp", s.path)
	return err
}

func (s *State) saveTo(path string) error {
	w, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer w.Close()
	encoder := json.NewEncoder(w)
	err = encoder.Encode(s)
	if err != nil {
		return err
	}
	_ = w.Sync()
	return nil
}
