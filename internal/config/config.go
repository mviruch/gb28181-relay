package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level YAML configuration.
type Config struct {
	Listen  ListenConfig   `yaml:"listen"`
	ZLM     ZLMConfig      `yaml:"zlm"`
	Devices []DeviceConfig `yaml:"devices"`
}

type ListenConfig struct {
	Host       string `yaml:"host"`       // bind address, e.g. 0.0.0.0
	Port       int    `yaml:"port"`       // SIP listen port
	PublicHost string `yaml:"publicHost"` // advertised Contact/SDP host
}

type ZLMConfig struct {
	API    string `yaml:"api"`    // e.g. http://127.0.0.1:80
	Secret string `yaml:"secret"` // ZLM HTTP API secret
	VHost  string `yaml:"vhost"`  // default __defaultVhost__
	App    string `yaml:"app"`    // media app name, default gb28181
}

type DeviceConfig struct {
	DeviceID string          `yaml:"deviceId"`
	Name     string          `yaml:"name"`
	SIP      DeviceSIPConfig `yaml:"sip"`
	Channels []ChannelConfig `yaml:"channels"`
}

type DeviceSIPConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	ServerID   string `yaml:"serverId"`
	Domain     string `yaml:"domain"`
	Username   string `yaml:"username"` // digest user; empty => deviceId
	Password   string `yaml:"password"`
	Transport  string `yaml:"transport"`  // udp | tcp
	Expires    int    `yaml:"expires"`    // REGISTER expires seconds
	Keepalive  int    `yaml:"keepalive"`  // Keepalive MESSAGE interval seconds
	RegisterID string `yaml:"registerId"` // deprecated/ignored; REGISTER Request-URI is always serverId@domain
}

type ChannelConfig struct {
	ChannelID string `yaml:"channelId"`
	Name      string `yaml:"name"`
	FLVURL    string `yaml:"flvUrl"`
}

// Load reads and validates a YAML config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Listen.Host == "" {
		c.Listen.Host = "0.0.0.0"
	}
	if c.Listen.Port == 0 {
		c.Listen.Port = 5060
	}
	if c.Listen.PublicHost == "" {
		if c.Listen.Host != "0.0.0.0" && c.Listen.Host != "::" {
			c.Listen.PublicHost = c.Listen.Host
		} else {
			c.Listen.PublicHost = "127.0.0.1"
		}
	}
	if c.ZLM.VHost == "" {
		c.ZLM.VHost = "__defaultVhost__"
	}
	if c.ZLM.App == "" {
		c.ZLM.App = "gb28181"
	}
	for i := range c.Devices {
		d := &c.Devices[i]
		if d.SIP.Port == 0 {
			d.SIP.Port = 5060
		}
		if d.SIP.Transport == "" {
			d.SIP.Transport = "udp"
		}
		d.SIP.Transport = strings.ToLower(d.SIP.Transport)
		if d.SIP.Expires <= 0 {
			d.SIP.Expires = 3600
		}
		if d.SIP.Keepalive <= 0 {
			d.SIP.Keepalive = 60
		}
		if d.SIP.Username == "" {
			d.SIP.Username = d.DeviceID
		}
		if d.SIP.Domain == "" && len(d.SIP.ServerID) >= 10 {
			d.SIP.Domain = d.SIP.ServerID[:10]
		}
		// registerId is ignored; sipua uses serverId@domain as REGISTER Request-URI.
		if d.Name == "" {
			d.Name = d.DeviceID
		}
		for j := range d.Channels {
			ch := &d.Channels[j]
			if ch.Name == "" {
				ch.Name = ch.ChannelID
			}
		}
	}
}

func (c *Config) validate() error {
	if c.ZLM.API == "" {
		return fmt.Errorf("zlm.api is required")
	}
	if len(c.Devices) == 0 {
		return fmt.Errorf("devices[] must not be empty")
	}
	seenDev := map[string]struct{}{}
	seenCh := map[string]struct{}{}
	for i, d := range c.Devices {
		if d.DeviceID == "" {
			return fmt.Errorf("devices[%d].deviceId is required", i)
		}
		if _, ok := seenDev[d.DeviceID]; ok {
			return fmt.Errorf("duplicate deviceId %q", d.DeviceID)
		}
		seenDev[d.DeviceID] = struct{}{}
		if d.SIP.Host == "" {
			return fmt.Errorf("devices[%d].sip.host is required", i)
		}
		if d.SIP.ServerID == "" {
			return fmt.Errorf("devices[%d].sip.serverId is required", i)
		}
		if d.SIP.Password == "" {
			return fmt.Errorf("devices[%d].sip.password is required", i)
		}
		if d.SIP.Transport != "udp" && d.SIP.Transport != "tcp" {
			return fmt.Errorf("devices[%d].sip.transport must be udp|tcp", i)
		}
		if len(d.Channels) == 0 {
			return fmt.Errorf("devices[%d].channels must not be empty", i)
		}
		for j, ch := range d.Channels {
			if ch.ChannelID == "" {
				return fmt.Errorf("devices[%d].channels[%d].channelId is required", i, j)
			}
			if ch.FLVURL == "" {
				return fmt.Errorf("devices[%d].channels[%d].flvUrl is required", i, j)
			}
			if _, ok := seenCh[ch.ChannelID]; ok {
				return fmt.Errorf("duplicate channelId %q", ch.ChannelID)
			}
			seenCh[ch.ChannelID] = struct{}{}
		}
	}
	return nil
}

// ListenAddr returns host:port for SIP bind.
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.Host, c.Listen.Port)
}
