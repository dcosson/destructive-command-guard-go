package cloud

import "github.com/dcosson/destructive-command-guard-go/internal/packs"

const (
	sevLow      = 1
	sevMedium   = 2
	sevHigh     = 3
	sevCritical = 4

	confLow    = 0
	confMedium = 1
	confHigh   = 2
)

func awsPack() packs.Pack {
	return packs.Pack{
		ID:          "cloud.aws",
		Name:        "AWS CLI",
		Description: "AWS CLI destructive operations",
		Keywords:    []string{"aws"},
		Safe: []packs.Rule{
			{ID: "aws-describe-safe", Match: packs.And(packs.Name("aws"), packs.ArgContains("describe"))},
		},
		Destructive: []packs.Rule{
			{ID: "aws-ec2-terminate", Severity: sevCritical, Confidence: confHigh, EnvSensitive: true, Reason: "EC2 terminate-instances permanently deletes running compute instances", Remediation: "Stop instances instead of terminating them", Match: packs.And(packs.Name("aws"), packs.ArgSubsequence("ec2", "terminate-instances"))},
		},
	}
}
