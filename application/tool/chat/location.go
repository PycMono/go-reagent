package chat

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/PycMono/go-reagent/domain/service"
)

type locationInput struct {
	Location    string `json:"location"`
	CountryCode string `json:"country_code,omitempty"`
	Admin1      string `json:"admin1,omitempty"`
}

type locationView struct {
	Name        string `json:"name"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Admin1      string `json:"admin1"`
	Timezone    string `json:"timezone"`
}

type locationFailure struct {
	Status     string         `json:"status"`
	Query      string         `json:"query"`
	Candidates []locationView `json:"candidates,omitempty"`
}

func resolveLocation(ctx context.Context, resolver service.LocationResolver, input locationInput) (service.Location, *locationFailure, error) {
	if err := ctx.Err(); err != nil {
		return service.Location{}, nil, err
	}
	locationName := strings.TrimSpace(input.Location)
	if locationName == "" {
		return service.Location{}, nil, invalidArguments("location is required", errors.New("location is required"))
	}
	if utf8.RuneCountInString(locationName) > 120 {
		return service.Location{}, nil, invalidArguments("location is too long", errors.New("location must not exceed 120 characters"))
	}

	countryCode := strings.ToUpper(strings.TrimSpace(input.CountryCode))
	if countryCode != "" && !isASCIICountryCode(countryCode) {
		return service.Location{}, nil, invalidArguments("invalid country code", errors.New("country_code must contain two ASCII letters"))
	}
	admin1 := strings.TrimSpace(input.Admin1)
	if input.Admin1 != "" && admin1 == "" {
		return service.Location{}, nil, invalidArguments("invalid admin area", errors.New("admin1 must not be blank"))
	}
	if utf8.RuneCountInString(admin1) > 120 {
		return service.Location{}, nil, invalidArguments("admin area is too long", errors.New("admin1 must not exceed 120 characters"))
	}

	query := service.LocationQuery{Name: locationName, CountryCode: countryCode, Admin1: admin1, Limit: 5}
	locations, err := resolver.ResolveLocations(ctx, query)
	if err != nil {
		return service.Location{}, nil, err
	}
	filtered := make([]service.Location, 0, len(locations))
	for _, location := range locations {
		if countryCode != "" && !strings.EqualFold(location.CountryCode, countryCode) {
			continue
		}
		if admin1 != "" && !strings.EqualFold(location.Admin1, admin1) {
			continue
		}
		filtered = append(filtered, location)
		if len(filtered) == 5 {
			break
		}
	}
	if len(filtered) == 0 {
		return service.Location{}, &locationFailure{Status: "not_found", Query: locationName}, nil
	}
	if len(filtered) > 1 {
		candidates := make([]locationView, len(filtered))
		for i, location := range filtered {
			candidates[i] = locationToView(location)
		}
		return service.Location{}, &locationFailure{Status: "ambiguous", Query: locationName, Candidates: candidates}, nil
	}
	return filtered[0], nil, nil
}

func isASCIICountryCode(value string) bool {
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
}

func locationToView(location service.Location) locationView {
	return locationView{
		Name: location.Name, Country: location.Country, CountryCode: location.CountryCode,
		Admin1: location.Admin1, Timezone: location.Timezone,
	}
}
