/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package types

import (
	"fmt"
	"time"
)

// FlexibleTime supports both date-only (YYYY-MM-DD) and full RFC3339 (YYYY-MM-DDTHH:MM:SSZ) formats
// +kubebuilder:validation:Type=string
type FlexibleTime string

// Time returns the parsed time.Time value
func (ft FlexibleTime) Time() (time.Time, error) {
	str := string(ft)
	if str == "" {
		return time.Time{}, nil
	}

	// Try RFC3339 format first
	if t, err := time.Parse(time.RFC3339, str); err == nil {
		return t, nil
	}

	// Try date-only format (YYYY-MM-DD)
	if t, err := time.Parse("2006-01-02", str); err == nil {
		return t.UTC(), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time %q: expected RFC3339 (2006-01-02T15:04:05Z) or date-only (2006-01-02) format", str)
}

// GetEndOfDay returns a FlexibleTime set to the end of the day (23:59:59.999999999Z)
// for the same date as the receiver
func (ft FlexibleTime) GetEndOfDay() (FlexibleTime, error) {
	t, err := ft.Time()
	if err != nil {
		return "", err
	}
	year, month, day := t.Date()
	endOfDay := time.Date(year, month, day, 23, 59, 59, 999999999, time.UTC)
	return FlexibleTime(endOfDay.Format(time.RFC3339)), nil
}
