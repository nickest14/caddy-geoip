package geoip

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/mmcloughlin/geohash"
	"github.com/oschwald/maxminddb-golang"
	"go.uber.org/zap"
)

// Geoip represents a middleware instance
type Geoip struct {
	BlockList struct {
		Country []string
		IP      []net.IP
	}
	AllowList struct {
		AllowOnly bool
		Country   []string
		IP        []net.IP
	}
	RequestIP      net.IP
	RequestCountry struct {
		Name string
		Code string
	}
	DatabasePath string
	DBHandler    *maxminddb.Reader
	logger       *zap.Logger
}

// geoIPRecord struct
type geoIPRecord struct {
	Country struct {
		ISOCode           string            `maxminddb:"iso_code"`
		IsInEuropeanUnion bool              `maxminddb:"is_in_european_union"`
		Names             map[string]string `maxminddb:"names"`
		GeoNameID         uint64            `maxminddb:"geoname_id"`
	} `maxminddb:"country"`

	City struct {
		Names     map[string]string `maxminddb:"names"`
		GeoNameID uint64            `maxminddb:"geoname_id"`
	} `maxminddb:"city"`

	Location struct {
		Latitude  float64 `maxminddb:"latitude"`
		Longitude float64 `maxminddb:"longitude"`
		TimeZone  string  `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}

func init() {
	caddy.RegisterModule(Geoip{})
	httpcaddyfile.RegisterHandlerDirective("geoip", parseCaddyfile)
}

// CaddyModule returns the Caddy module information.
func (Geoip) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.geoip",
		New: func() caddy.Module { return new(Geoip) },
	}
}

// Provision implements caddy.Provisioner.
func (g *Geoip) Provision(ctx caddy.Context) error {
	dbhandler, err := maxminddb.Open(g.DatabasePath)
	if err != nil {
		return fmt.Errorf("geoip: Can't open database: " + g.DatabasePath)
	}
	g.DBHandler = dbhandler
	g.logger = ctx.Logger(g) // g.logger is a *zap.Logger
	return nil
}

// ServeHTTP implements caddyhttp.GeoipHandler.
func (g Geoip) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	g.lookupLocation(w, r)
	if g.AllowList.AllowOnly {
		// Only ip in Allowlist can visit the website.
		goto CheckAllowList
	} else {
		for _, country := range g.BlockList.Country {
			if country == g.RequestCountry.Code {
				goto CheckAllowList
			}
		}
		for _, ip := range g.BlockList.IP {
			if ip.String() == g.RequestIP.String() {
				goto CheckAllowList
			}
		}
		return next.ServeHTTP(w, r)
	}

CheckAllowList:
	for _, country := range g.AllowList.Country {
		if country == g.RequestCountry.Code {
			return next.ServeHTTP(w, r)
		}
	}
	for _, ip := range g.AllowList.IP {
		if ip.String() == g.RequestIP.String() {
			return next.ServeHTTP(w, r)
		}
	}
	return caddyhttp.Error(http.StatusForbidden, fmt.Errorf("forbidden"))
}

func (g *Geoip) lookupLocation(w http.ResponseWriter, r *http.Request) {
	record := g.fetchGeoipData(r)
	g.RequestCountry.Code = record.Country.ISOCode
	g.RequestCountry.Name = record.Country.Names["en"]
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	repl.Set("geoip_country_code", record.Country.ISOCode)
	repl.Set("geoip_country_name", record.Country.Names["en"])
	repl.Set("geoip_country_eu", strconv.FormatBool(record.Country.IsInEuropeanUnion))
	repl.Set("geoip_country_geoname_id", strconv.FormatUint(record.Country.GeoNameID, 10))
	repl.Set("geoip_city_name", record.City.Names["en"])
	repl.Set("geoip_city_geoname_id", strconv.FormatUint(record.City.GeoNameID, 10))
	repl.Set("geoip_latitude", strconv.FormatFloat(record.Location.Latitude, 'f', 6, 64))
	repl.Set("geoip_longitude", strconv.FormatFloat(record.Location.Longitude, 'f', 6, 64))
	repl.Set("geoip_geohash", geohash.Encode(record.Location.Latitude, record.Location.Longitude))
	repl.Set("geoip_time_zone", record.Location.TimeZone)
}

func (g *Geoip) fetchGeoipData(r *http.Request) geoIPRecord {
	clientIP, _ := getClientIP(r)
	g.RequestIP = clientIP
	var record = geoIPRecord{}
	err := g.DBHandler.Lookup(clientIP, &record)
	if err != nil {
		g.logger.Warn("Lookup IP error: err", zap.String("err", err.Error()))
	}

	if record.Country.ISOCode == "" {
		record.Country.Names = make(map[string]string)
		record.City.Names = make(map[string]string)
		if clientIP.IsLoopback() {
			record.Country.ISOCode = "**"
			record.Country.Names["en"] = "Loopback"
			record.City.Names["en"] = "Loopback"
		} else {
			record.Country.ISOCode = "!!"
			record.Country.Names["en"] = "No Country"
			record.City.Names["en"] = "No City"
		}
	}
	return record
}

func getClientIP(r *http.Request) (net.IP, error) {
	var ip string

	// Use the client ip from the 'X-Forwarded-For' header, if available.
	if fwdFor := r.Header.Get("X-Forwarded-For"); fwdFor != "" {
		ips := strings.Split(fwdFor, ", ")
		ip = ips[0]
	} else {
		// Otherwise, get the client ip from the request remote address.
		var err error
		ip, _, err = net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			if serr, ok := err.(*net.AddrError); ok && serr.Err == "missing port in address" {
				// It's not critical try parse
				ip = r.RemoteAddr
			} else {
				log.Printf("Error when SplitHostPort: %v", serr.Err)
				return nil, err
			}
		}
	}
	// Parse the ip address string into a net IP.
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return nil, errors.New("unable to parse address")
	}
	return parsedIP, nil
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (g *Geoip) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		args := d.RemainingArgs()
		if len(args) != 1 {
			return d.ArgErr()
		}
		g.DatabasePath = args[0]

		for d.NextBlock(0) {
			switch d.Val() {
			case "block_list":
				for d.NextBlock(1) {
					switch d.Val() {
					case "country":
						args := d.RemainingArgs()
						g.BlockList.Country = append(g.BlockList.Country, args...)
					case "ip":
						args := d.RemainingArgs()
						for _, ip := range args {
							g.BlockList.IP = append(g.BlockList.IP, net.ParseIP(ip))
						}
					default:
						return d.Errf("unrecognized subdirective in geoip block_list %s", d.Val())
					}
				}
			case "allow_list":
				for d.NextBlock(1) {
					switch d.Val() {
					case "allow_only":
						args := d.RemainingArgs()
						allowOnly, err := strconv.ParseBool(args[0])
						if len(args) != 1 || err != nil {
							return d.ArgErr()
						}
						g.AllowList.AllowOnly = allowOnly
					case "country":
						args := d.RemainingArgs()
						g.AllowList.Country = append(g.AllowList.Country, args...)
					case "ip":
						args := d.RemainingArgs()
						for _, ip := range args {
							g.AllowList.IP = append(g.AllowList.IP, net.ParseIP(ip))
						}
					default:
						return d.Errf("unrecognized subdirective in geoip allow_list %s", d.Val())
					}
				}
			default:
				return d.Errf("unrecognized subdirective in geoip plugin %s", d.Val())
			}
		}
	}
	return nil
}

// parseCaddyfile unmarshals tokens from h into a new Geoip.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Geoip
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return m, err
}

// Interface guards
var (
	_ caddy.Provisioner           = (*Geoip)(nil)
	_ caddyhttp.MiddlewareHandler = (*Geoip)(nil)
	_ caddyfile.Unmarshaler       = (*Geoip)(nil)
)
