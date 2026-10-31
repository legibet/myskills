package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/store"
)

// Config returns config.json.
func (s *Service) Config() (store.Config, error) {
	return store.LoadConfig(s.paths.Config)
}

// Agents returns the built-in and custom agents.
func (s *Service) Agents() ([]agent.Agent, error) {
	cfg, err := s.Config()
	if err != nil {
		return nil, err
	}
	return agent.All(cfg.CustomAgents), nil
}

// SetDefaultAgents sets the agents used for new enablements.
func (s *Service) SetDefaultAgents(ids []string) error {
	if len(ids) == 0 {
		return errors.New("choose at least one agent")
	}
	return s.mutate(func(st *state) error {
		var defaults []string
		for _, id := range ids {
			if _, ok := agent.Find(st.agents, id); !ok {
				return errorf(ErrNotFound, "unknown agent %q", id)
			}
			if !slices.Contains(defaults, id) {
				defaults = append(defaults, id)
			}
		}
		st.cfg.Agents = defaults
		return store.SaveConfig(s.paths.Config, st.cfg)
	})
}

// AddAgent adds a custom agent. An empty Name defaults to the id.
func (s *Service) AddAgent(a agent.Agent) error {
	if a.Name == "" {
		a.Name = a.ID
	}
	a.GlobalEnv = "" // built-ins only
	if err := agent.ValidateCustom(a); err != nil {
		return err
	}
	return s.mutate(func(st *state) error {
		if _, ok := agent.Find(st.agents, a.ID); ok {
			return errorf(ErrConflict, "agent %q already exists", a.ID)
		}
		st.cfg.CustomAgents = append(st.cfg.CustomAgents, a)
		return store.SaveConfig(s.paths.Config, st.cfg)
	})
}

// RemoveAgent removes a custom agent after removing its link placements in
// every known scope (copies are released). It also drops the agent from the
// defaults. It returns the placements that were removed.
//
// Placements in a directory the agent shares with another agent stay.
func (s *Service) RemoveAgent(id string) ([]string, error) {
	var removed []string
	err := s.mutate(func(st *state) error {
		i := slices.IndexFunc(st.cfg.CustomAgents, func(a agent.Agent) bool { return a.ID == id })
		if i < 0 {
			if _, ok := agent.Find(agent.Builtins, id); ok {
				return fmt.Errorf("%s is a built-in agent and can't be removed", id)
			}
			return errorf(ErrNotFound, "unknown agent %q", id)
		}

		var errs []error
		var touched []string
		for _, sc := range knownScopes(st.lock) {
			for _, d := range agentDirs(st.agents, sc) {
				if !slices.Equal(d.agents, []string{id}) {
					continue
				}
				placements, err := s.scanDir(st.lock, d)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				for _, p := range placements {
					if p.Kind != KindLink && p.Kind != KindBroken {
						continue
					}
					if err := removePlacement(st.lock, p); err != nil {
						errs = append(errs, err)
						continue
					}
					removed = append(removed, p.Path)
					if !sc.IsGlobal() && !slices.Contains(touched, sc.Root) {
						touched = append(touched, sc.Root)
					}
				}
				// Release the copies: the files stay, unmanaged.
				for key := range st.lock.Copies {
					if slices.ContainsFunc(d.aliases, func(alias string) bool { return filepath.Dir(key) == alias }) {
						delete(st.lock.Copies, key)
					}
				}
			}
		}
		if err := errors.Join(errs...); err != nil {
			return err // keep the agent so that removing it can be retried
		}

		st.cfg.CustomAgents = slices.Delete(st.cfg.CustomAgents, i, i+1)
		st.cfg.Agents = slices.DeleteFunc(st.cfg.Agents, func(a string) bool { return a == id })
		if err := store.SaveConfig(s.paths.Config, st.cfg); err != nil {
			return err
		}
		st.agents = agent.All(st.cfg.CustomAgents)
		for _, root := range touched {
			s.syncExclude(st, root) // best effort: the removed links are gone either way
		}
		s.pruneProjects(st)
		return nil
	})
	return removed, err
}

// Projects returns the known project roots.
func (s *Service) Projects() ([]string, error) {
	lock, err := store.LoadLock(s.paths.Lock)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(slices.Values(lock.Projects)), nil
}
