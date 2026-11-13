package main

import (
	"errors"
	"familyrobot/internal/management"
	"familyrobot/internal/music"
	"familyrobot/internal/realtimelive"
	"time"
)

// Composition owns trusted CLI paths and binding lookup; catalogues own storage
// and playback resources. Never use client-supplied family IDs to scope music.
func configureMusic(cfg *realtimelive.Config, online bool, root, configPath, memorySocket string) error {
	if (root != "" || memorySocket != "") && configPath == "" {
		return errors.New("management_config_required")
	}
	if root != "" || memorySocket != "" {
		if _, err := management.LoadConfig(configPath); err != nil {
			return errors.New("management_config_invalid")
		}
	}
	cfg.Music = nil
	cfg.MusicForDevice = nil
	if online {
		cfg.Music = music.New()
	}
	if root == "" {
		return nil
	}
	base := music.NewPrivateOnly()
	if online {
		base = cfg.Music
	}
	cfg.Music = base
	cfg.MusicForDevice = func(deviceID string) *music.Catalog {
		current, err := management.LoadConfig(configPath)
		if err != nil {
			return nil
		}
		binding, ok := current.CurrentBinding(deviceID, time.Now())
		if !ok {
			return nil
		}
		allowed := func() bool {
			latest, err := management.LoadConfig(configPath)
			if err != nil {
				return false
			}
			b, ok := latest.CurrentBinding(deviceID, time.Now())
			return ok && b.ID == binding.ID && b.Version == binding.Version && b.FamilyID == binding.FamilyID && b.DeviceID == binding.DeviceID && b.From.Equal(binding.From)
		}
		library, err := music.NewLibrary(root, binding.FamilyID)
		if err != nil {
			return nil
		}
		return base.WithLibrary(library.WithAccessCheck(allowed), allowed)
	}
	return nil
}
