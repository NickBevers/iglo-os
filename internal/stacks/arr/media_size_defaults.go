package arr

import "fmt"

type appQualitySize struct {
	Min       float64
	Max       *float64
	Preferred *float64
}

// Sonarr v4.0.20.3014 and Radarr v6.4.4.10685, checked 2026-11-08.
// Names come from Sonarr-Quality.cs:78 and Radarr-Quality.cs:80. Defaults
// come from Sonarr-Quality.cs:156 and Radarr-Quality.cs:168 in the supplied
// upstream qualitydef sources. Keep null max and preferred values as null.
// These are app defaults, independent of the moving TRaSH guide.
var pinnedQualitySizes = map[string]map[string]appQualitySize{
	"sonarr": {
		"Unknown":            {Min: 1, Max: qualitySizeNumber(199.9), Preferred: qualitySizeNumber(95)},
		"SDTV":               {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBRip-480p":        {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBDL-480p":         {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"DVD":                {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-480p":        {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-576p":        {Min: 2, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"HDTV-720p":          {Min: 3, Max: qualitySizeNumber(125), Preferred: qualitySizeNumber(95)},
		"HDTV-1080p":         {Min: 4, Max: qualitySizeNumber(125), Preferred: qualitySizeNumber(95)},
		"Raw-HD":             {Min: 4, Max: nil, Preferred: qualitySizeNumber(95)},
		"WEBRip-720p":        {Min: 3, Max: qualitySizeNumber(130), Preferred: qualitySizeNumber(95)},
		"WEBDL-720p":         {Min: 3, Max: qualitySizeNumber(130), Preferred: qualitySizeNumber(95)},
		"Bluray-720p":        {Min: 4, Max: qualitySizeNumber(130), Preferred: qualitySizeNumber(95)},
		"WEBRip-1080p":       {Min: 4, Max: qualitySizeNumber(130), Preferred: qualitySizeNumber(95)},
		"WEBDL-1080p":        {Min: 4, Max: qualitySizeNumber(130), Preferred: qualitySizeNumber(95)},
		"Bluray-1080p":       {Min: 4, Max: qualitySizeNumber(155), Preferred: qualitySizeNumber(95)},
		"Bluray-1080p Remux": {Min: 35, Max: nil, Preferred: qualitySizeNumber(95)},
		"HDTV-2160p":         {Min: 35, Max: qualitySizeNumber(199.9), Preferred: qualitySizeNumber(95)},
		"WEBRip-2160p":       {Min: 35, Max: nil, Preferred: qualitySizeNumber(95)},
		"WEBDL-2160p":        {Min: 35, Max: nil, Preferred: qualitySizeNumber(95)},
		"Bluray-2160p":       {Min: 35, Max: nil, Preferred: qualitySizeNumber(95)},
		"Bluray-2160p Remux": {Min: 35, Max: nil, Preferred: qualitySizeNumber(95)},
	},
	"radarr": {
		"Unknown":      {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WORKPRINT":    {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"CAM":          {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"TELESYNC":     {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"TELECINE":     {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"REGIONAL":     {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"DVDSCR":       {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"SDTV":         {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"DVD":          {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"DVD-R":        {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBDL-480p":   {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBRip-480p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-480p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-576p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"HDTV-720p":    {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBDL-720p":   {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBRip-720p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-720p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"HDTV-1080p":   {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBDL-1080p":  {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"WEBRip-1080p": {Min: 0, Max: qualitySizeNumber(100), Preferred: qualitySizeNumber(95)},
		"Bluray-1080p": {Min: 0, Max: nil, Preferred: nil},
		"Remux-1080p":  {Min: 0, Max: nil, Preferred: nil},
		"HDTV-2160p":   {Min: 0, Max: nil, Preferred: nil},
		"WEBDL-2160p":  {Min: 0, Max: nil, Preferred: nil},
		"WEBRip-2160p": {Min: 0, Max: nil, Preferred: nil},
		"Bluray-2160p": {Min: 0, Max: nil, Preferred: nil},
		"Remux-2160p":  {Min: 0, Max: nil, Preferred: nil},
		"BR-DISK":      {Min: 0, Max: nil, Preferred: nil},
		"Raw-HD":       {Min: 0, Max: nil, Preferred: nil},
	},
}

func qualitySizeNumber(value float64) *float64 { return &value }

func qualitySizeValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func defaultQualitySize(service, name string) (appQualitySize, error) {
	defaults, ok := pinnedQualitySizes[service][name]
	if !ok {
		return appQualitySize{}, fmt.Errorf("%s quality %q is absent from the pinned app defaults; update Iglo-OS before managing this quality", service, name)
	}
	return defaults, nil
}
