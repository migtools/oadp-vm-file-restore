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

package constant

import "testing"

// TestBooleanStringConstants verifies that boolean string constants follow Kubernetes conventions.
// CRITICAL: These MUST be lowercase to ensure:
// 1. Compatibility with kubevirt-velero-plugin (expects lowercase "true")
// 2. Kubernetes label/annotation conventions (lowercase boolean values)
// 3. Proper namespace cleanup (temp namespace label matching)
//
// DO NOT change these to capital case ("True"/"False") as it will break:
// - PVC creation (kubevirt-velero-plugin won't recognize annotations)
// - Namespace cleanup (label matching will fail)
// - External integrations expecting standard Kubernetes conventions
//
// See issue #44 and related fixes for context.
func TestBooleanStringConstants(t *testing.T) {
	tests := []struct {
		name          string
		constantValue string
		expectedValue string
		reason        string
	}{
		{
			name:          "TrueString must be lowercase",
			constantValue: TrueString,
			expectedValue: "true",
			reason:        "Kubernetes conventions and kubevirt-velero-plugin expect lowercase 'true'",
		},
		{
			name:          "FalseString must be lowercase",
			constantValue: FalseString,
			expectedValue: "false",
			reason:        "Kubernetes conventions expect lowercase 'false'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constantValue != tt.expectedValue {
				t.Errorf("CRITICAL: %s has incorrect value.\n"+
					"  Got:      %q\n"+
					"  Expected: %q\n"+
					"  Reason:   %s\n"+
					"  Impact:   Changing this will break kubevirt-velero-plugin integration and namespace cleanup.\n"+
					"  See:      Issue #44 and related PR for details.",
					tt.name, tt.constantValue, tt.expectedValue, tt.reason)
			}
		})
	}
}

// TestTrueStringNotCapitalized ensures TrueString is not capitalized.
// This test will fail if someone accidentally changes "true" to "True".
func TestTrueStringNotCapitalized(t *testing.T) {
	if TrueString == "True" {
		t.Error("CRITICAL: TrueString must be lowercase 'true', not 'True'. " +
			"Capital case breaks kubevirt-velero-plugin and namespace cleanup. " +
			"See issue #44.")
	}

	if TrueString == "TRUE" {
		t.Error("CRITICAL: TrueString must be lowercase 'true', not 'TRUE'. " +
			"All-caps breaks Kubernetes conventions.")
	}
}

// TestFalseStringNotCapitalized ensures FalseString is not capitalized.
// This test will fail if someone accidentally changes "false" to "False".
func TestFalseStringNotCapitalized(t *testing.T) {
	if FalseString == "False" {
		t.Error("CRITICAL: FalseString must be lowercase 'false', not 'False'. " +
			"Capital case breaks Kubernetes conventions.")
	}

	if FalseString == "FALSE" {
		t.Error("CRITICAL: FalseString must be lowercase 'false', not 'FALSE'. " +
			"All-caps breaks Kubernetes conventions.")
	}
}

// TestBooleanStringConstantsUsageInLabels verifies that TrueString works correctly
// in label comparisons (the original bug that caused namespace cleanup to fail).
func TestBooleanStringConstantsUsageInLabels(t *testing.T) {
	// Simulate how the constants are used in production code
	labels := map[string]string{
		VMFRTempNamespaceLabel: TrueString,
		ManagedByLabel:         ManagedByLabelValue,
	}

	// Verify the label value is lowercase (critical for namespace cleanup)
	if labels[VMFRTempNamespaceLabel] != "true" {
		t.Errorf("VMFRTempNamespaceLabel should be set to lowercase 'true', got %q. "+
			"This will break namespace cleanup logic.", labels[VMFRTempNamespaceLabel])
	}

	// Verify comparison works (this is how cleanup code checks for temp namespaces)
	isTempNamespace := labels[VMFRTempNamespaceLabel] == TrueString
	if !isTempNamespace {
		t.Error("Temp namespace detection failed. Label comparison doesn't work correctly.")
	}
}

// TestBooleanStringConstantsUsageInAnnotations verifies that TrueString works correctly
// in Velero Restore annotations (critical for kubevirt-velero-plugin).
func TestBooleanStringConstantsUsageInAnnotations(t *testing.T) {
	// Simulate how the constants are used in Velero Restore annotations
	annotations := map[string]string{
		"oadp.openshift.io/vmfr-restore": TrueString,
	}

	// Verify the annotation value is lowercase (critical for kubevirt-velero-plugin)
	if annotations["oadp.openshift.io/vmfr-restore"] != "true" {
		t.Errorf("vmfr-restore annotation should be lowercase 'true', got %q. "+
			"This will break kubevirt-velero-plugin PVC uniquification.",
			annotations["oadp.openshift.io/vmfr-restore"])
	}
}
