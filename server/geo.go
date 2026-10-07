package main

import (
	"fmt"
	"math"
	"strings"
)

// site is one iperf3 test point. Keep this in the same order as
// IPERF_SERVERS in nodebench.sh.
type site struct {
	Location, Provider, Host, Ports string
	Region                          string
	Lon, Lat                        float64
	Ours                            bool
	// Extended sites only run with -x
	Extended bool
}

var sites = []site{
	{"Eygelshoven, NL", "InstantNode", "lg.instantnode.eu", "5201", "eu", 6.06, 50.89, true, false},
	{"London, UK", "Clouvider", "lon.speedtest.clouvider.net", "5200-5209", "eu", -0.13, 51.51, false, false},
	{"Amsterdam, NL", "Eranium", "iperf-ams-nl.eranium.net", "5201-5210", "eu", 4.9, 52.37, false, false},
	{"Frankfurt, DE", "Leaseweb", "speedtest.fra1.de.leaseweb.net", "5201-5210", "eu", 8.68, 50.11, false, false},
	{"Paris, FR", "Moji", "iperf3.moji.fr", "5200-5240", "eu", 2.35, 48.86, false, false},
	{"Hamburg, DE", "wilhelm.tel", "speedtest.wtnet.de", "5200-5209", "eu", 9.99, 53.55, false, true},
	{"New York, US", "Leaseweb", "speedtest.nyc1.us.leaseweb.net", "5201-5210", "na", -74.0, 40.71, false, false},
	{"Chicago, US", "Leaseweb", "speedtest.chi11.us.leaseweb.net", "5201-5210", "na", -87.63, 41.88, false, true},
	{"Miami, US", "Leaseweb", "speedtest.mia11.us.leaseweb.net", "5201-5210", "na", -80.19, 25.76, false, true},
	{"Montreal, CA", "Leaseweb", "speedtest.mtl2.ca.leaseweb.net", "5201-5210", "na", -73.57, 45.5, false, true},
	{"Dallas, US", "Leaseweb", "speedtest.dal13.us.leaseweb.net", "5201-5210", "na", -96.8, 32.78, false, false},
	{"Los Angeles, US", "Clouvider", "la.speedtest.clouvider.net", "5200-5209", "na", -118.24, 34.05, false, false},
	{"Sao Paulo, BR", "Edgoo", "speedtest.sao1.edgoo.net", "9204-9240", "sa", -46.63, -23.55, false, false},
	{"Singapore, SG", "Leaseweb", "speedtest.sin1.sg.leaseweb.net", "5201-5210", "asia", 103.82, 1.35, false, false},
	{"Hong Kong, HK", "Leaseweb", "speedtest.hkg12.hk.leaseweb.net", "5201-5210", "asia", 114.17, 22.32, false, true},
	{"Tokyo, JP", "Leaseweb", "speedtest.tyo11.jp.leaseweb.net", "5201-5210", "asia", 139.69, 35.69, false, false},
	{"Sydney, AU", "Leaseweb", "speedtest.syd12.au.leaseweb.net", "5201-5210", "oc", 151.21, -33.87, false, false},
}

// standardSites counts the sites a normal run tests against.
func standardSites() int {
	n := 0
	for _, s := range sites {
		if !s.Extended {
			n++
		}
	}
	return n
}

var regionNames = map[string]string{"eu": "Europe", "na": "North America", "sa": "South America", "asia": "Asia", "oc": "Oceania"}

func siteFor(location string) *site {
	for i := range sites {
		if sites[i].Location == location {
			return &sites[i]
		}
	}
	return nil
}

// Rough country centres, only used to draw where a result came from.
// Countries missing here just don't get the lines on the map.
var countryPos = map[string][2]float64{
	"DE": {10.4, 51.2}, "NL": {5.3, 52.1}, "GB": {-1.5, 52.6}, "UK": {-1.5, 52.6}, "FR": {2.4, 46.6},
	"BE": {4.5, 50.6}, "LU": {6.1, 49.8}, "CH": {8.2, 46.8}, "AT": {14.5, 47.6}, "PL": {19.4, 52.1},
	"CZ": {15.3, 49.8}, "DK": {9.3, 56.0}, "SE": {16.0, 62.0}, "NO": {9.0, 61.0}, "FI": {26.0, 63.0},
	"ES": {-3.6, 40.2}, "PT": {-8.0, 39.6}, "IT": {12.6, 42.8}, "IE": {-8.0, 53.2}, "RO": {25.0, 45.9},
	"BG": {25.2, 42.7}, "HU": {19.4, 47.2}, "LT": {23.9, 55.3}, "LV": {24.9, 56.9}, "EE": {25.0, 58.7},
	"UA": {31.2, 49.0}, "TR": {35.2, 39.0}, "RU": {40.0, 56.0}, "IS": {-18.6, 64.9}, "GR": {22.0, 39.3},
	"US": {-98.6, 39.8}, "CA": {-100.0, 56.0}, "MX": {-102.5, 23.6}, "BR": {-51.9, -10.0},
	"AR": {-64.0, -34.0}, "CL": {-71.0, -33.5}, "CO": {-74.3, 4.6}, "ZA": {24.7, -29.0},
	"EG": {30.8, 26.8}, "NG": {8.7, 9.1}, "KE": {37.9, 0.0}, "AE": {54.3, 24.0}, "SA": {45.1, 23.9},
	"IL": {34.9, 31.0}, "IN": {79.0, 22.0}, "SG": {103.8, 1.35}, "HK": {114.2, 22.3}, "JP": {138.3, 36.2},
	"KR": {127.8, 36.4}, "CN": {104.2, 35.9}, "TW": {121.0, 23.7}, "VN": {106.0, 16.0}, "TH": {101.0, 15.0},
	"MY": {102.0, 4.2}, "ID": {113.9, -0.8}, "PH": {122.0, 12.9}, "AU": {134.0, -25.0}, "NZ": {172.5, -41.0},
}

// Map projection: plain equirectangular, cut at 80N and 58S because
// nothing we draw lives near the poles.
const (
	mapW            = 720.0
	mapTop, mapBot  = 80.0, -58.0
	mapH            = (mapTop - mapBot) * mapW / 360
	mapDot, mapStep = 1.7, 3.0
	// the zoomed in layer: a finer grid, only loaded once someone zooms
	fineDot, fineStep = 0.75, 1.25
)

func project(lon, lat float64) (x, y float64) {
	return (lon + 180) * mapW / 360, (mapTop - lat) * mapW / 360
}

// Very rough coastlines in lon/lat. They only decide which dots of the
// grid are land, so a few degrees of error disappear in the dot pattern.
var land = [][]float64{
	// north america
	{-168, 66, -162, 70, -150, 71, -140, 70, -128, 70, -115, 68, -100, 68, -95, 72, -85, 70, -80, 63, -90, 58, -82, 52, -78, 58, -70, 60, -62, 58, -56, 52, -60, 47, -66, 44, -70, 42, -75, 38, -76, 35, -81, 31, -80, 25, -83, 29, -90, 30, -97, 27, -97, 22, -91, 19, -87, 21, -88, 16, -83, 10, -79, 9, -80, 7, -85, 10, -92, 14, -105, 20, -110, 24, -115, 30, -118, 34, -124, 40, -124, 48, -130, 54, -140, 60, -152, 58, -160, 56, -165, 60},
	{-85, 22, -74, 20, -77, 19.8, -80, 23},
	{-55, 60, -45, 60, -35, 66, -20, 70, -18, 77, -30, 83, -60, 82, -70, 77, -58, 70, -53, 66},
	{-24, 64, -14, 64, -14, 66, -22, 66.5},
	// south america
	{-80, 9, -75, 11, -62, 11, -52, 5, -50, 0, -35, -5, -35, -9, -39, -15, -41, -22, -48, -26, -53, -34, -58, -38, -65, -42, -68, -50, -70, -55, -75, -50, -73, -40, -71, -30, -70, -18, -76, -14, -81, -6, -80, 0, -78, 7},
	// europe
	{-10, 36, -9, 43, -2, 43.5, -4, 48, 2, 51, 5, 53, 8, 54, 9, 57, 11, 59, 5, 58, 5, 62, 14, 68, 20, 70, 28, 71, 32, 69, 30, 65, 24, 65, 22, 60, 28, 60, 30, 56, 40, 56, 45, 48, 40, 43, 30, 41, 26, 40, 23, 36, 20, 40, 18, 40, 16, 38, 13, 42, 10, 44, 6, 43, 3, 42, 0, 39, -2, 37, -6, 36},
	{-6, 50, 1, 51, 2, 53, -1, 55, -3, 58.5, -6, 58, -5, 55, -3, 54, -4.5, 52},
	{-10, 52, -6, 52, -6, 55, -8, 55.3, -10, 54},
	// africa
	{-17, 21, -16, 28, -9, 32, -6, 36, 10, 37, 11, 33, 20, 31, 32, 31, 34, 28, 38, 18, 43, 12, 51, 12, 44, 2, 40, -5, 40, -15, 35, -25, 32, -29, 27, -34, 20, -35, 18, -32, 14, -23, 12, -17, 13, -8, 9, -1, 9, 4, 5, 6, -4, 5, -8, 4, -13, 8, -17, 14},
	{44, -25, 47, -25, 50, -15, 49, -12, 44, -16},
	// asia
	{26, 40, 36, 36, 35, 32, 32, 31, 35, 28, 43, 13, 52, 16, 56, 22, 60, 22, 62, 25, 67, 24, 72, 20, 77, 8, 80, 13, 80, 16, 86, 21, 92, 22, 94, 17, 98, 16, 98, 8, 100, 4, 104, 1.3, 104, 10, 109, 12, 108, 17, 106, 20, 110, 21, 117, 24, 122, 30, 121, 32, 119, 38, 122, 40, 126, 38, 129, 35, 129, 41, 132, 43, 140, 48, 141, 53, 137, 55, 143, 59, 156, 61, 162, 58, 156, 51, 163, 60, 178, 63, 180, 65, 180, 70, 165, 70, 150, 72, 140, 73, 130, 72, 115, 74, 105, 78, 95, 77, 80, 73, 70, 73, 68, 69, 60, 69, 50, 68, 44, 68, 33, 70, 28, 71, 32, 69, 30, 65, 24, 65, 22, 60, 28, 60, 30, 56, 40, 56, 45, 48, 40, 43, 30, 41},
	{130, 31, 135, 34, 140, 35, 142, 40, 141, 45, 145, 44, 140, 42, 139, 38, 136, 36, 131, 34},
	{120, 18, 122, 18, 126, 7, 122, 7},
	{95, 5, 98, 4, 106, -6, 104, -5},
	{109, 1, 117, 7, 119, 1, 116, -4, 110, -3},
	{105, -6, 114, -7, 114, -8.5, 106, -7.5},
	{131, -1, 141, -2.5, 150, -10, 141, -9, 137, -5},
	// oceania
	{114, -22, 122, -18, 130, -12, 137, -12, 136, -15, 141, -11, 146, -19, 153, -26, 150, -37, 141, -38, 135, -35, 131, -31, 124, -34, 115, -34},
	{172, -35, 178, -38, 175, -41, 168, -46, 172, -42},
}

func onLand(lon, lat float64) bool {
	for _, p := range land {
		in := false
		for i, j := 0, len(p)-2; i < len(p); j, i = i, i+2 {
			xi, yi, xj, yj := p[i], p[i+1], p[j], p[j+1]
			if (yi > lat) != (yj > lat) && lon < (xj-xi)*(lat-yi)/(yj-yi)+xi {
				in = !in
			}
		}
		if in {
			return true
		}
	}
	return false
}

// worldSVG draws the dotted base map once at start; it's served as a
// static file and pulled into the charts with <image>.
func worldSVG(step, dot float64) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f"><path fill="#2c2c31" d="`, mapW, mapH)
	for lat := mapTop - step/2; lat > mapBot; lat -= step {
		for lon := -180 + step/2; lon < 180; lon += step {
			if onLand(lon, lat) {
				x, y := project(lon, lat)
				fmt.Fprintf(&b, "M%.2f %.2fh%.2fv%.2fh-%.2fz", x-dot/2, y-dot/2, dot, dot, dot)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return []byte(b.String())
}

// arc is a gentle curve between two map points, bent upwards so the lines
// to far away sites don't all lie on top of each other.
func arc(x1, y1, x2, y2 float64) string {
	mx, my := (x1+x2)/2, (y1+y2)/2
	d := math.Hypot(x2-x1, y2-y1)
	return fmt.Sprintf("M%.1f %.1fQ%.1f %.1f %.1f %.1f", x1, y1, mx, my-d*0.25, x2, y2)
}
