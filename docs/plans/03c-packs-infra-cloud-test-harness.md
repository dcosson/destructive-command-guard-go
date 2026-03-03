# 03c: Infrastructure & Cloud Packs — Test Harness

**Plan**: [03c-packs-infra-cloud.md](./03c-packs-infra-cloud.md)
**Architecture**: [00-architecture.md](./00-architecture.md)
**Core Pack Test Harness**: [03a-packs-core-test-harness.md](./03a-packs-core-test-harness.md)

---

## Overview

This document specifies the test harness for the infrastructure and cloud
packs (plan 03c). It covers property-based tests, deterministic examples,
fault injection, comparison oracles, benchmarks, stress tests, security
tests, manual QA, CI tier mapping, and exit criteria.

The test harness complements the unit tests described in the plan doc §7.
Unit tests verify individual pattern behavior. This harness verifies
system-level properties, cross-pattern interactions, and robustness.

Infrastructure and cloud packs introduce testing challenges not present
in core or database packs:
- **Subcommand depth**: Commands use 1-3 levels of positional args for
  subcommands. Off-by-one errors in `ArgAt()` indices can cause false
  negatives.
- **Universal environment sensitivity**: ALL 58 destructive patterns are
  env-sensitive. Every pattern must be tested for escalation behavior.
- **Auto-approve severity split**: Terraform and Pulumi have dual patterns
  for the same action (with/without auto-approve). Both variants must be
  tested and the severity difference verified.
- **Ansible content matching**: Module arguments via `-a` flag values
  require flag-value-aware matching (`SQLContent`) similar to the database
  pack SQL-content approach.
- **AWS breadth**: 15 destructive patterns across 7 services. Cross-service
  isolation must be verified.

---

## P: Property-Based Tests

### P1: Every Destructive Pattern Has a Matching Command

**Invariant**: For each destructive pattern in each infra/cloud pack, there
exists at least one `ExtractedCommand` that the pattern matches.

Same property as 03a P1, extended to 6 packs. Uses the reachability
command map from plan doc §7.2.

```go
func TestPropertyEveryInfraCloudDestructivePatternReachable(t *testing.T) {
    packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            t.Run(pack.ID+"/"+dp.Name, func(t *testing.T) {
                cmd := getReachabilityCommand(pack.ID, dp.Name)
                assert.True(t, dp.Match.Match(cmd),
                    "pattern %s has no matching reachability command", dp.Name)
            })
        }
    }
}
```

### P2: Safe Patterns Never Match Destructive Reachability Commands

**Invariant**: For each destructive pattern's reachability command, no safe
pattern in the same pack matches it.

This is critical for terraform where S4 (`terraform-readonly-safe`)
includes `state` and `workspace` as safe subcommands but must exclude
destructive state/workspace subcommands via positional matching:
`Not(ArgAt(1, "rm"))`, `Not(ArgAt(1, "mv"))`, `Not(ArgAt(1, "push"))`,
`Not(ArgAt(1, "delete"))`.

```go
func TestPropertyInfraCloudSafePatternsNeverBlockDestructive(t *testing.T) {
    packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            cmd := getReachabilityCommand(pack.ID, dp.Name)
            for _, sp := range pack.Safe {
                assert.False(t, sp.Match.Match(cmd),
                    "safe pattern %s blocks destructive %s in pack %s",
                    sp.Name, dp.Name, pack.ID)
            }
        }
    }
}
```

### P3: Universal Environment Sensitivity

**Invariant**: Every destructive pattern in every infra/cloud pack has
`EnvSensitive: true`.

This is a blanket policy for the infra/cloud category — no exceptions.

```go
func TestPropertyUniversalEnvSensitivity(t *testing.T) {
    packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            assert.True(t, dp.EnvSensitive,
                "%s/%s must be env-sensitive", pack.ID, dp.Name)
        }
    }
}
```

### P4: Auto-Approve Severity Escalation

**Invariant**: For Terraform and Pulumi, auto-approve variants of the same
action always have strictly higher severity than their prompted counterparts.

```go
func TestPropertyAutoApproveSeverityEscalation(t *testing.T) {
    pairs := []struct {
        packID       string
        autoApprove  string
        prompted     string
    }{
        {"infrastructure.terraform", "terraform-destroy-auto-approve", "terraform-destroy"},
        {"infrastructure.terraform", "terraform-apply-auto-approve", "terraform-apply"},
        {"infrastructure.pulumi", "pulumi-destroy-yes", "pulumi-destroy"},
        {"infrastructure.pulumi", "pulumi-up-yes", "pulumi-up"},
    }

    for _, pair := range pairs {
        t.Run(pair.autoApprove, func(t *testing.T) {
            pack := packByID(pair.packID)
            autoDP := findDestructiveByName(pack, pair.autoApprove)
            promptDP := findDestructiveByName(pack, pair.prompted)
            assert.Greater(t, int(autoDP.Severity), int(promptDP.Severity),
                "auto-approve %s should have higher severity than prompted %s",
                pair.autoApprove, pair.prompted)
        })
    }
}
```

### P5: No Destructive Pattern Matches Empty Command

**Invariant**: An empty `ExtractedCommand` matches no destructive pattern
in any infra/cloud pack.

```go
func TestPropertyInfraCloudEmptyCommandMatchesNothing(t *testing.T) {
    empty := parse.ExtractedCommand{}
    packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
    for _, pack := range packs {
        for _, dp := range pack.Destructive {
            assert.False(t, dp.Match.Match(empty),
                "pattern %s/%s matches empty command", pack.ID, dp.Name)
        }
    }
}
```

### P6: Cross-Pack Pattern Isolation

**Invariant**: A Terraform command does not trigger Pulumi, Ansible, AWS,
GCP, or Azure patterns, and vice versa.

```go
func TestPropertyCrossPackIsolation(t *testing.T) {
    packCommands := map[string]parse.ExtractedCommand{
        "infrastructure.terraform": cmd("terraform", []string{"destroy"}, m("-auto-approve", "")),
        "infrastructure.pulumi":    cmd("pulumi", []string{"destroy"}, m("--yes", "")),
        "infrastructure.ansible":   cmd("ansible", []string{"all"}, m("-m", "file", "-a", "state=absent")),
        "cloud.aws":                cmd("aws", []string{"ec2", "terminate-instances"}, nil),
        "cloud.gcp":                cmd("gcloud", []string{"projects", "delete", "my-proj"}, nil),
        "cloud.azure":              cmd("az", []string{"group", "delete"}, nil),
    }

    dbPacks := map[string]packs.Pack{
        "infrastructure.terraform": terraformPack,
        "infrastructure.pulumi":    pulumiPack,
        "infrastructure.ansible":   ansiblePack,
        "cloud.aws":                awsPack,
        "cloud.gcp":                gcpPack,
        "cloud.azure":              azurePack,
    }

    for cmdPackID, testCmd := range packCommands {
        for packID, pack := range dbPacks {
            if packID == cmdPackID {
                continue
            }
            for _, dp := range pack.Destructive {
                assert.False(t, dp.Match.Match(testCmd),
                    "%s command triggers %s/%s",
                    cmdPackID, packID, dp.Name)
            }
        }
    }
}
```

### P7: AWS Service Isolation

**Invariant**: An AWS EC2 command does not trigger RDS, S3, CloudFormation,
IAM, Lambda, or ECS patterns, and vice versa.

```go
func TestPropertyAWSServiceIsolation(t *testing.T) {
    serviceCommands := map[string]parse.ExtractedCommand{
        "ec2":            cmd("aws", []string{"ec2", "terminate-instances"}, nil),
        "rds":            cmd("aws", []string{"rds", "delete-db-instance"}, nil),
        "s3":             cmd("aws", []string{"s3", "rb", "s3://bucket"}, m("--force", "")),
        "cloudformation": cmd("aws", []string{"cloudformation", "delete-stack"}, nil),
        "iam":            cmd("aws", []string{"iam", "delete-role"}, nil),
        "lambda":         cmd("aws", []string{"lambda", "delete-function"}, nil),
        "ecs":            cmd("aws", []string{"ecs", "delete-service"}, nil),
    }

    servicePatternPrefixes := map[string]string{
        "ec2": "aws-ec2-", "rds": "aws-rds-", "s3": "aws-s3-",
        "cloudformation": "aws-cfn-", "iam": "aws-iam-",
        "lambda": "aws-lambda-", "ecs": "aws-ecs-",
    }

    for svcName, testCmd := range serviceCommands {
        for _, dp := range awsPack.Destructive {
            if strings.HasPrefix(dp.Name, servicePatternPrefixes[svcName]) {
                continue // Same service — expected to match
            }
            assert.False(t, dp.Match.Match(testCmd),
                "%s command triggers pattern %s", svcName, dp.Name)
        }
    }
}
```

### P8: Ansible Flag-Value Content Matching Contract

**Invariant**: Ansible module and module-argument detection must work when
content appears in flag values (`-m`, `-a`, `--extra-vars`), not only in
positional args.

```go
func TestPropertyAnsibleFlagValueMatching(t *testing.T) {
    cmd1 := cmd("ansible", []string{"all"}, m("-m", "file", "-a", "state=absent"))
    assert.True(t, packs.SQLContent("file").Match(cmd1))
    assert.True(t, packs.SQLContent("state=absent").Match(cmd1))

    cmd2 := cmd("ansible-playbook", []string{"site.yml"}, m("--extra-vars", "state=absent"))
    assert.True(t, packs.SQLContent("state=absent").Match(cmd2))
}
```

---

## E: Deterministic Examples

### E1: infrastructure.terraform Pattern Matrix (25 cases)

See plan doc §6.1 for the full golden file entries.

```
# Critical
terraform destroy -auto-approve                     → Deny/Critical

# High
terraform destroy                                   → Deny/High
terraform apply -auto-approve                       → Deny/High

# Medium
terraform apply                                     → Ask/Medium
terraform state rm module.vpc                       → Ask/Medium
terraform state mv module.old module.new            → Ask/Medium
terraform state push local.tfstate                  → Ask/Medium
terraform workspace delete staging                  → Ask/Medium
terraform taint aws_instance.web                    → Ask/Medium

# Safe
terraform plan                                      → Allow
terraform init                                      → Allow
terraform validate                                  → Allow
terraform fmt                                       → Allow
terraform output                                    → Allow
terraform state list                                → Allow
terraform state show aws_instance.web               → Allow
terraform state pull                                → Allow
terraform graph                                     → Allow
terraform providers                                 → Allow
terraform version                                   → Allow
terraform show                                      → Allow
terraform state list aws_instance.webfarm           → Allow (P0-1 regression test)
terraform import aws_instance.web i-1234            → Ask/Indeterminate (no explicit safe/destructive matcher in v1)
```

### E2: infrastructure.pulumi Pattern Matrix (16 cases)

See plan doc §6.2 for the full golden file entries.

```
# Critical
pulumi destroy --yes                                → Deny/Critical
pulumi destroy -y                                   → Deny/Critical

# High
pulumi destroy                                      → Deny/High
pulumi stack rm my-stack                            → Deny/High
pulumi up --yes                                     → Deny/High

# Medium
pulumi up                                           → Ask/Medium
pulumi cancel                                       → Ask/Medium

# Safe
pulumi preview                                      → Allow
pulumi stack ls                                     → Allow
pulumi stack output                                 → Allow
pulumi config set key value                         → Allow
pulumi login                                        → Allow
pulumi whoami                                       → Allow
pulumi version                                      → Allow
pulumi about                                        → Allow
```

### E3: infrastructure.ansible Pattern Matrix (16 cases)

See plan doc §6.3 for the full golden file entries.

```
# Critical
ansible all -m shell -a 'rm -rf /'                  → Deny/Critical
ansible webservers -m command -a 'dd if=/dev/zero'  → Deny/Critical

# High
ansible all -m file -a 'state=absent'               → Deny/High
ansible all -m service -a 'state=stopped'           → Deny/High
ansible-playbook site.yml --extra-vars 'state=absent' → Deny/High

# Medium
ansible all -m user -a 'state=absent'               → Ask/Medium
ansible-playbook site.yml                           → Ask/Medium
ansible all -m apt -a 'state=absent'                → Ask/Medium

# Safe
ansible all -m ping                                 → Allow
ansible all -m setup                                → Allow
ansible-playbook site.yml --check                   → Allow
ansible-playbook site.yml --list-hosts              → Allow
ansible all -m stat -a 'path=/etc'                  → Allow
ansible all -m debug -a 'var=hostname'              → Allow
ansible-playbook site.yml --syntax-check            → Allow
ansible-playbook site.yml --list-tasks              → Allow

# Safe-pattern shadowing regression (R3 P1 fix)
ansible all -m shell -a 'rm -rf /tmp/setup'         → Deny/Critical (safe token in -a must NOT short-circuit)
ansible all -m setup -a 'rm -rf /tmp/data'           → Allow (genuine safe module, -a content irrelevant)
```

### E4: cloud.aws Pattern Matrix (30 cases)

See plan doc §6.4 for the full golden file entries.

### E5: cloud.gcp Pattern Matrix (24 cases)

See plan doc §6.5 for the full golden file entries.

### E6: cloud.azure Pattern Matrix (22 cases)

See plan doc §6.6 for the full golden file entries.

### E7: Cross-Pack Non-Interference (6 cases)

Verify that infra/cloud packs don't interfere with each other:

```
terraform destroy -auto-approve                     → Matches infrastructure.terraform only
pulumi destroy --yes                                → Matches infrastructure.pulumi only
ansible all -m shell -a 'rm -rf /'                  → Matches infrastructure.ansible only
aws ec2 terminate-instances --instance-ids i-1234   → Matches cloud.aws only
gcloud projects delete my-project                   → Matches cloud.gcp only
az group delete --name my-rg                        → Matches cloud.azure only
```

### E8: Auto-Approve Severity Edge Cases (8 cases)

```
# Terraform: -auto-approve flag position shouldn't matter
terraform destroy -target=module.vpc -auto-approve  → Deny/Critical (flag order irrelevant)
terraform -auto-approve destroy                     → depends on flag parsing (may not match)

# Pulumi: both --yes and -y
pulumi destroy --yes --target urn:pulumi::my-res    → Deny/Critical
pulumi destroy -y --target urn:pulumi::my-res       → Deny/Critical
pulumi up -y                                        → Deny/High
pulumi up --yes --skip-preview                      → Deny/High

# Terraform apply: severity split
terraform apply -auto-approve -var 'env=prod'       → Deny/High (not Critical)
terraform apply                                     → Ask/Medium (not High)
```

---

## F: Fault Injection

### F1: Nil/Empty Fields in ExtractedCommand

Test that all infra/cloud pack matchers handle degenerate inputs gracefully.

```go
func TestFaultInfraCloudNilFields(t *testing.T) {
    packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
    degenerateCmds := []parse.ExtractedCommand{
        {Name: "terraform", Args: nil, Flags: nil},
        {Name: "aws", Args: nil, Flags: nil},
        {Name: "gcloud", Args: nil, Flags: nil},
        {Name: "az", Args: nil, Flags: nil},
        {Name: "ansible", Args: nil, Flags: nil},
        {Name: "pulumi", Args: nil, Flags: nil},
        {Name: "terraform", Args: []string{}, Flags: map[string]string{}},
        {Name: "", Args: nil, Flags: nil},
        {Name: "aws", Args: []string{""}, Flags: nil},
    }

    for _, pack := range packs {
        for i, c := range degenerateCmds {
            t.Run(fmt.Sprintf("%s/degenerate-%d", pack.ID, i), func(t *testing.T) {
                for _, dp := range pack.Destructive {
                    assert.NotPanics(t, func() { dp.Match.Match(c) })
                }
                for _, sp := range pack.Safe {
                    assert.NotPanics(t, func() { sp.Match.Match(c) })
                }
            })
        }
    }
}
```

### F2: ArgAt Out of Bounds

Test that `ArgAt()` matchers handle commands with fewer args than expected
without panicking. This is especially important for cloud packs with 3-level
subcommands (e.g., `gcloud compute instances delete` requires ArgAt(2)).

```go
func TestFaultArgAtOutOfBounds(t *testing.T) {
    // Commands with insufficient args for deep ArgAt matchers
    shortCmds := []parse.ExtractedCommand{
        cmd("gcloud", nil, nil),                     // 0 args — ArgAt(0) out of bounds
        cmd("gcloud", []string{"compute"}, nil),     // 1 arg — ArgAt(1) out of bounds
        cmd("gcloud", []string{"compute", "instances"}, nil), // 2 args — ArgAt(2) out of bounds
        cmd("aws", nil, nil),                        // 0 args
        cmd("aws", []string{"ec2"}, nil),            // 1 arg — ArgAt(1) out of bounds
        cmd("az", nil, nil),                         // 0 args
        cmd("az", []string{"sql"}, nil),             // 1 arg
        cmd("az", []string{"sql", "server"}, nil),   // 2 args — ArgAt(2) out of bounds
    }

    packs := []packs.Pack{awsPack, gcpPack, azurePack}
    for _, pack := range packs {
        for i, c := range shortCmds {
            t.Run(fmt.Sprintf("%s/short-%d", pack.ID, i), func(t *testing.T) {
                for _, dp := range pack.Destructive {
                    assert.NotPanics(t, func() { dp.Match.Match(c) })
                }
            })
        }
    }
}
```

### F3: Ansible Module Argument Edge Cases

Test Ansible patterns with unusual `-a` flag values:

```go
func TestFaultAnsibleModuleArgs(t *testing.T) {
    edgeCases := []parse.ExtractedCommand{
        // Empty -a value
        cmd("ansible", []string{"all"}, m("-m", "file", "-a", "")),
        // state=absent as part of a longer value
        cmd("ansible", []string{"all"}, m("-m", "file", "-a", "path=/tmp state=absent recurse=yes")),
        // state=absent in module name (false positive test)
        cmd("ansible", []string{"all"}, m("-m", "state=absent", "-a", "path=/tmp")),
        // No -m flag at all
        cmd("ansible", []string{"all", "-a", "rm -rf /"}, nil),
    }

    for i, c := range edgeCases {
        t.Run(fmt.Sprintf("ansible-edge-%d", i), func(t *testing.T) {
            for _, dp := range ansiblePack.Destructive {
                assert.NotPanics(t, func() { dp.Match.Match(c) })
            }
        })
    }
}
```

---

## O: Comparison Oracle Tests

### O1: Upstream Rust Version Comparison

Compare infra/cloud pack results against the upstream Rust
`destructive-command-guard` for shared commands.

```go
func TestComparisonInfraCloudUpstreamRust(t *testing.T) {
    if testing.Short() {
        t.Skip("comparison tests require upstream binary")
    }
    corpus := loadComparisonCorpus(t, "testdata/comparison/infra_cloud_commands.txt")
    for _, entry := range corpus {
        t.Run(entry.Command, func(t *testing.T) {
            goResult := pipeline.Run(ctx, entry.Command, cfg)
            rustResult := runUpstream(t, entry.Command)

            if goResult.Decision != rustResult.Decision {
                t.Logf("DIVERGENCE: %q go=%v rust=%v category=%s",
                    entry.Command, goResult.Decision, rustResult.Decision,
                    categorizeDivergence(goResult, rustResult))
            }
        })
    }
}
```

**Comparison corpus**: All 132 golden file commands from §6 of the plan doc.

### O2: Cross-Cloud Consistency

For equivalent destructive operations across cloud providers, verify
severity assignments are consistent:

```go
func TestComparisonCrossCloudConsistency(t *testing.T) {
    equivalents := []struct {
        name     string
        commands map[string]parse.ExtractedCommand
    }{
        {
            "VM/Instance termination",
            map[string]parse.ExtractedCommand{
                "aws":   cmd("aws", []string{"ec2", "terminate-instances"}, nil),
                "gcp":   cmd("gcloud", []string{"compute", "instances", "delete", "vm"}, nil),
                "azure": cmd("az", []string{"vm", "delete"}, nil),
            },
        },
        {
            "Database deletion",
            map[string]parse.ExtractedCommand{
                "aws":   cmd("aws", []string{"rds", "delete-db-instance"}, nil),
                "gcp":   cmd("gcloud", []string{"sql", "instances", "delete", "db"}, nil),
                "azure": cmd("az", []string{"sql", "server", "delete"}, nil),
            },
        },
        {
            "Object storage deletion",
            map[string]parse.ExtractedCommand{
                "aws":   cmd("aws", []string{"s3", "rb", "s3://bucket"}, m("--force", "")),
                "gcp":   cmd("gsutil", []string{"rm", "gs://bucket"}, m("-r", "")),
                "azure": cmd("az", []string{"storage", "account", "delete"}, nil),
            },
        },
        {
            "Cluster deletion",
            map[string]parse.ExtractedCommand{
                "aws":   cmd("aws", []string{"ecs", "delete-cluster"}, nil),
                "gcp":   cmd("gcloud", []string{"container", "clusters", "delete", "c"}, nil),
                "azure": cmd("az", []string{"aks", "delete"}, nil),
            },
        },
        {
            "Container/project/stack deletion (Critical tier)",
            map[string]parse.ExtractedCommand{
                "aws":   cmd("aws", []string{"cloudformation", "delete-stack"}, nil),
                "gcp":   cmd("gcloud", []string{"projects", "delete", "my-proj"}, nil),
                "azure": cmd("az", []string{"group", "delete"}, nil),
            },
        },
    }

    for _, eq := range equivalents {
        t.Run(eq.name, func(t *testing.T) {
            var severities []guard.Severity
            for cloud, testCmd := range eq.commands {
                pack := cloudPackFor(cloud)
                for _, dp := range pack.Destructive {
                    if dp.Match.Match(testCmd) {
                        severities = append(severities, dp.Severity)
                        t.Logf("%s: severity=%v pattern=%s", cloud, dp.Severity, dp.Name)
                        break
                    }
                }
            }
            // All matching clouds should have same severity
            for i := 1; i < len(severities); i++ {
                assert.Equal(t, severities[0], severities[i],
                    "%s severity inconsistency across clouds", eq.name)
            }
        })
    }
}
```

### O3: IaC Tool Consistency

For equivalent IaC operations (Terraform vs Pulumi), verify severity
consistency:

```go
func TestComparisonIaCConsistency(t *testing.T) {
    equivalents := []struct {
        name     string
        severity guard.Severity
        commands map[string]parse.ExtractedCommand
    }{
        {
            "destroy with auto-approve",
            guard.Critical,
            map[string]parse.ExtractedCommand{
                "terraform": cmd("terraform", []string{"destroy"}, m("-auto-approve", "")),
                "pulumi":    cmd("pulumi", []string{"destroy"}, m("--yes", "")),
            },
        },
        {
            "destroy without auto-approve",
            guard.High,
            map[string]parse.ExtractedCommand{
                "terraform": cmd("terraform", []string{"destroy"}, nil),
                "pulumi":    cmd("pulumi", []string{"destroy"}, nil),
            },
        },
        {
            "apply/up with auto-approve",
            guard.High,
            map[string]parse.ExtractedCommand{
                "terraform": cmd("terraform", []string{"apply"}, m("-auto-approve", "")),
                "pulumi":    cmd("pulumi", []string{"up"}, m("--yes", "")),
            },
        },
        {
            "apply/up without auto-approve",
            guard.Medium,
            map[string]parse.ExtractedCommand{
                "terraform": cmd("terraform", []string{"apply"}, nil),
                "pulumi":    cmd("pulumi", []string{"up"}, nil),
            },
        },
    }

    for _, eq := range equivalents {
        t.Run(eq.name, func(t *testing.T) {
            for tool, testCmd := range eq.commands {
                pack := packByTool(tool)
                matched := false
                for _, dp := range pack.Destructive {
                    if dp.Match.Match(testCmd) {
                        assert.Equal(t, eq.severity, dp.Severity,
                            "%s %s severity mismatch", tool, eq.name)
                        matched = true
                        break
                    }
                }
                assert.True(t, matched, "%s should match for %s", tool, eq.name)
            }
        })
    }
}
```

### O4: Policy Monotonicity

For each infra/cloud golden file entry, verify that stricter policies never
allow what looser policies deny:

```go
func TestComparisonInfraCloudPolicyMonotonicity(t *testing.T) {
    entries := golden.LoadCorpus(t, "testdata/golden/infra_cloud_*.txt")
    restrictiveness := map[guard.Decision]int{guard.Allow: 0, guard.Ask: 1, guard.Deny: 2}

    for _, entry := range entries {
        t.Run(entry.Description, func(t *testing.T) {
            strict := pipeline.Run(ctx, entry.Command, strictCfg)
            inter := pipeline.Run(ctx, entry.Command, interCfg)
            perm := pipeline.Run(ctx, entry.Command, permCfg)

            sr := restrictiveness[strict.Decision]
            ir := restrictiveness[inter.Decision]
            pr := restrictiveness[perm.Decision]

            assert.GreaterOrEqual(t, sr, ir)
            assert.GreaterOrEqual(t, ir, pr)
        })
    }
}
```

---

## B: Benchmarks

### B1: Per-Pack Subcommand Matching Throughput

Infrastructure/cloud packs use `ArgAt()` positional matching which should
be faster than regex-based database pack matching.

```go
func BenchmarkTerraformPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-plan":      cmd("terraform", []string{"plan"}, nil),
        "destroy":        cmd("terraform", []string{"destroy"}, nil),
        "destroy-auto":   cmd("terraform", []string{"destroy"}, m("-auto-approve", "")),
        "apply":          cmd("terraform", []string{"apply"}, nil),
        "state-rm":       cmd("terraform", []string{"state", "rm", "module.vpc"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(terraformPack, c)
            }
        })
    }
}

func BenchmarkAWSPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-describe":  cmd("aws", []string{"ec2", "describe-instances"}, nil),
        "ec2-terminate":  cmd("aws", []string{"ec2", "terminate-instances"}, nil),
        "s3-rb-force":    cmd("aws", []string{"s3", "rb", "s3://bucket"}, m("--force", "")),
        "s3-ls":          cmd("aws", []string{"s3", "ls"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(awsPack, c)
            }
        })
    }
}

func BenchmarkGCPPackMatch(b *testing.B) {
    commands := map[string]parse.ExtractedCommand{
        "safe-list":      cmd("gcloud", []string{"compute", "instances", "list"}, nil),
        "instances-del":  cmd("gcloud", []string{"compute", "instances", "delete", "vm"}, nil),
        "projects-del":   cmd("gcloud", []string{"projects", "delete", "my-proj"}, nil),
        "gsutil-rm":      cmd("gsutil", []string{"rm", "gs://bucket/file"}, nil),
    }
    for name, c := range commands {
        b.Run(name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                matchPack(gcpPack, c)
            }
        })
    }
}
```

**Targets** (initial — adjust after baseline measurement):
- Safe pattern match (short-circuit): < 100ns per command
- Destructive pattern match (ArgAt): < 300ns per command
- Full pack evaluation (all patterns): < 500ns per command

Infrastructure/cloud packs should be faster than database packs because
they use `ArgAt()` (O(1) index access) instead of regex matching.

### B2: Infra/Cloud Golden File Corpus Throughput

```go
func BenchmarkInfraCloudGoldenCorpus(b *testing.B) {
    entries := golden.LoadCorpus(b, "testdata/golden/infra_cloud_*.txt")
    pipeline := setupBenchPipeline(b)
    cfg := &evalConfig{policy: InteractivePolicy()}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, e := range entries {
            pipeline.Run(context.Background(), e.Command, cfg)
        }
    }
}
```

**Target**: Full infra/cloud corpus (132 entries) < 2ms total.

### B3: AWS Pack Scaling

Benchmark the AWS pack specifically because it has the most patterns (15
destructive + 3 safe = 18 total):

```go
func BenchmarkAWSFullPackEval(b *testing.B) {
    // Worst case: command that matches no safe and no destructive pattern
    noMatch := cmd("aws", []string{"dynamodb", "put-item"}, nil)
    b.Run("no-match-worst-case", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            matchPack(awsPack, noMatch)
        }
    })
}
```

---

## S: Stress Tests

### S1: Concurrent Infra/Cloud Pack Matching

Verify all packs are safe for concurrent use:

```go
func TestStressConcurrentInfraCloudMatching(t *testing.T) {
    var wg sync.WaitGroup
    commands := []parse.ExtractedCommand{
        cmd("terraform", []string{"destroy"}, m("-auto-approve", "")),
        cmd("aws", []string{"ec2", "terminate-instances"}, nil),
        cmd("gcloud", []string{"projects", "delete", "my-proj"}, nil),
        cmd("az", []string{"group", "delete"}, nil),
        cmd("ansible", []string{"all"}, m("-m", "file", "-a", "state=absent")),
        cmd("pulumi", []string{"destroy"}, m("--yes", "")),
    }

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(idx int) {
            defer wg.Done()
            c := commands[idx%len(commands)]
            packs := []packs.Pack{terraformPack, pulumiPack, ansiblePack, awsPack, gcpPack, azurePack}
            for j := 0; j < 1000; j++ {
                for _, pack := range packs {
                    for _, dp := range pack.Destructive {
                        dp.Match.Match(c)
                    }
                    for _, sp := range pack.Safe {
                        sp.Match.Match(c)
                    }
                }
            }
        }(i)
    }
    wg.Wait()
}
```

### S2: High-Volume Mixed Infra/Cloud Commands

```go
func TestStressHighVolumeInfraCloudCommands(t *testing.T) {
    allCommands := loadAllGoldenCommands(t, "testdata/golden/infra_cloud_*.txt")

    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func(worker int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                cmd := allCommands[(worker*100+j)%len(allCommands)]
                result := pipeline.Run(context.Background(), cmd.Command, interCfg)
                _ = result
            }
        }(i)
    }
    wg.Wait()
}
```

---

## SEC: Security Tests

### SEC1: Subcommand Evasion Attempts

Test that subcommand matching cannot be evaded by inserting extra arguments
or using alternative syntax:

```go
func TestSecuritySubcommandEvasion(t *testing.T) {
    tests := []struct {
        name     string
        cmd      parse.ExtractedCommand
        packID   string
        wantDeny bool
        reason   string
    }{
        // Extra args before subcommand
        {"terraform --var-file=prod.tfvars destroy",
            cmd("terraform", []string{"destroy"}, m("--var-file", "prod.tfvars")),
            "infrastructure.terraform", true,
            "flags before subcommand should not affect matching"},
        // AWS with --profile flag
        {"aws --profile prod ec2 terminate-instances",
            cmd("aws", []string{"ec2", "terminate-instances"}, m("--profile", "prod")),
            "cloud.aws", true,
            "profile flag should not affect matching"},
        // GCP with --project flag
        {"gcloud --project prod compute instances delete vm",
            cmd("gcloud", []string{"compute", "instances", "delete", "vm"}, m("--project", "prod")),
            "cloud.gcp", true,
            "project flag should not affect matching"},
        // AWS flag between subcommand levels
        {"aws ec2 --debug terminate-instances",
            cmd("aws", []string{"ec2", "terminate-instances"}, m("--debug", "")),
            "cloud.aws", true,
            "flags between subcommand levels should not affect matching"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            pack := packByID(tt.packID)
            matched := false
            for _, dp := range pack.Destructive {
                if dp.Match.Match(tt.cmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched, tt.reason)
        })
    }
}
```

### SEC2: Ansible Content Injection

Test that Ansible module argument matching handles injection-like content:

```go
func TestSecurityAnsibleContentInjection(t *testing.T) {
    tests := []struct {
        name     string
        cmd      parse.ExtractedCommand
        wantDeny bool
    }{
        // state=absent in file path (false positive — acceptable)
        {"path containing state=absent",
            cmd("ansible", []string{"all"}, m("-m", "file", "-a", "path=/tmp/state=absent.txt")),
            true},
        // Multiple -a flags
        {"multiple -a flags",
            cmd("ansible", []string{"all"}, m("-m", "file", "-a", "state=absent")),
            true},
        // state=present (not destructive)
        {"state=present",
            cmd("ansible", []string{"all"}, m("-m", "file", "-a", "path=/tmp state=present")),
            false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            matched := false
            for _, dp := range ansiblePack.Destructive {
                if dp.Match.Match(tt.cmd) {
                    matched = true
                    break
                }
            }
            assert.Equal(t, tt.wantDeny, matched)
        })
    }
}
```

### SEC3: Environment Sensitivity Preconditions

Verify env-sensitive patterns correctly escalate:

```go
func TestSecurityEnvSensitivityPreConditions(t *testing.T) {
    // Sample from each pack
    envSensitivePatterns := []struct {
        packID       string
        pattern      string
        baseSeverity guard.Severity
    }{
        {"infrastructure.terraform", "terraform-destroy-auto-approve", guard.Critical},
        {"infrastructure.terraform", "terraform-apply", guard.Medium},
        {"infrastructure.pulumi", "pulumi-destroy-yes", guard.Critical},
        {"cloud.aws", "aws-ec2-terminate", guard.High},
        {"cloud.gcp", "gcloud-projects-delete", guard.Critical},
        {"cloud.azure", "az-group-delete", guard.Critical},
        {"infrastructure.ansible", "ansible-shell-destructive", guard.Critical},
    }

    for _, tt := range envSensitivePatterns {
        t.Run(tt.packID+"/"+tt.pattern, func(t *testing.T) {
            pack := packByID(tt.packID)
            dp := findDestructiveByName(pack, tt.pattern)
            assert.True(t, dp.EnvSensitive, "pattern should be env-sensitive")
            assert.Equal(t, tt.baseSeverity, dp.Severity, "base severity mismatch")
        })
    }
}
```

---

## MQ: Manual QA Plan

### MQ1: Real-World Infra/Cloud Command Evaluation

Test with commands from actual LLM coding sessions involving infrastructure:

1. Collect 20 infrastructure-related Bash tool invocations from agent logs:
   - Terraform plan/apply cycles
   - AWS describe/list operations for debugging
   - GCP instance creation and configuration
   - Ansible ad-hoc commands for server inspection
   - Azure resource listing and management
2. Run each through the pipeline with `InteractivePolicy`
3. Verify:
   - No false positives on read-only operations (describe, list, plan)
   - All destructive commands are caught
   - Auto-approve variants get higher severity
   - Interactive sessions (without auto-approve) get lower severity
   - `--check`/`--dryrun` modes are allowed

### MQ2: Cross-Cloud Consistency Review

Manually verify equivalent operations get consistent treatment:
1. VM/instance termination — all three clouds should be High
2. Database deletion — all three clouds should be High
3. Storage deletion — all three clouds should be High for bucket/account,
   Medium for single object
4. Cluster deletion — all three clouds should be High
5. Project/resource group deletion — Critical for GCP (gcloud projects
   delete) and Azure (az group delete); no AWS equivalent (AWS accounts
   are managed differently)

### MQ3: Terraform State Management Review

Manually verify terraform state commands:
1. `terraform state list` → Allow (safe)
2. `terraform state show resource` → Allow (safe)
3. `terraform state rm resource` → Ask/Medium (destructive)
4. `terraform state mv a b` → Ask/Medium (destructive)
5. `terraform state pull` → Allow (safe, via S4 readonly)
6. `terraform state push` → Ask/Medium (terraform-state-push)
7. `terraform workspace delete staging` → Ask/Medium (terraform-workspace-delete)

### MQ4: AWS Service Coverage Review

Manually evaluate AWS CLI commands not covered in v1 to prioritize v2:
1. `aws dynamodb delete-table` — should be covered in v2
2. `aws elasticache delete-cluster` — should be covered in v2
3. `aws eks delete-cluster` — should be covered in v2
4. `aws route53 delete-hosted-zone` — should be covered in v2
5. `aws sns delete-topic` — lower priority
6. `aws sqs delete-queue` — lower priority
7. Document coverage gaps for v2 planning

---

## CI Tier Mapping

| Tier | Tests | Trigger |
|------|-------|---------|
| T1 (Fast, every commit) | P1-P8, E1-E8, F1-F3, SEC1-SEC3 | Every commit |
| T2 (Standard, every PR) | T1 + B1-B3, S1-S2 | PR open/update |
| T3 (Extended, nightly) | T1 + T2 + O1-O4 | Nightly schedule |
| T4 (Manual, pre-release) | MQ1-MQ4 | Before each release |

**T1 time budget**: < 10 seconds (faster than database packs — no regex)
**T2 time budget**: < 30 seconds
**T3 time budget**: < 5 minutes (includes upstream comparison + cross-cloud oracle)

---

## Exit Criteria

### Must Pass

1. **All property tests pass** — P1-P8
2. **All deterministic examples pass** — E1-E8
3. **All fault injection tests pass** — F1-F3
4. **All security tests pass** — SEC1-SEC3
5. **Golden file corpus passes** — All 132 infra/cloud entries
6. **Pattern reachability 100%** — Every destructive pattern reachable across
   all 6 packs (58 patterns total)
7. **Cross-pack isolation verified** — P6 passes
8. **Universal env sensitivity verified** — P3 passes for all 6 packs
9. **Auto-approve severity split verified** — P4 passes
10. **No data races** — S1 passes with -race flag
11. **Zero panics in any test** — Including F1-F3 fault injection

### Should Pass

12. **Benchmarks recorded** — B1-B3 have baseline values
13. **Stress tests pass** — S1-S2 complete without issues
14. **Comparison oracle baseline** — O1 has initial divergence report
15. **Cross-cloud consistency** — O2 has no unexpected severity differences
16. **IaC tool consistency** — O3 passes

### Tracked Metrics

- Pattern count by pack (safe + destructive) — target: 17 safe + 58 destructive
  across 6 packs
- Test count by category (unit, reachability, golden, property, security)
- Golden file entry count — target: 132 entries across 6 packs
- ArgAt matching latency per pattern (from B1)
- Environment sensitivity coverage: 6 of 6 packs (100%)
- Upstream comparison divergence count and categorization
- Cross-cloud severity consistency: 0 unexpected differences
- AWS pattern count by service

---

## Round 1 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-reviewer | P1 | P2 comment references Not(ArgContent("rm")) | Incorporated | Updated P2 comment to reference ArgAt(1) positional matching |
| 2 | dcg-reviewer | P1 | Missing golden entries for safe terraform ops | Incorporated | E1 updated from 16 to 25 cases |
| 3 | dcg-reviewer | P1 | MQ3 terraform state push no conclusion | Incorporated | MQ3 updated with expected behavior |
| 4 | dcg-alt-reviewer | P1 | P4 auto-approve test missing pulumi up pair | Incorporated | P4 updated with pulumi-up-yes/pulumi-up pair |
| 5 | dcg-alt-reviewer | P2 | Golden file count 118 | Incorporated | All counts updated to 132 |
| 6 | dcg-alt-reviewer | P3 | O2 missing container deletion tier | Incorporated | O2 added container/project/stack deletion tier |
| 7 | dcg-alt-reviewer | P3 | SEC1 flag ordering test coverage | Incorporated | SEC1 added aws flag-between-subcommands test |
| 8 | N/A | N/A | Pattern and golden count updates | Incorporated | E1-E5, B2-B3, O1, O3, exit criteria updated for new patterns |

## Round 2 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | domain-packs-r2 | P2 | Missing diagnostic test for Ansible flag-value content matching | Incorporated | Added P8 property test for `-m`/`-a`/`--extra-vars` flag-value matching contract |
| 2 | domain-packs-r2 | P2 | Missing `terraform import` expected-behavior case | Incorporated | Added explicit E1 case as Ask/Indeterminate in current v1 behavior |
| 3 | domain-packs-r2 | P3 | SEC3 name implied escalation behavior while testing static preconditions | Incorporated | Renamed SEC3 heading and function to preconditions wording |

## Round 3 Review Disposition

| # | Reviewer | Severity | Summary | Disposition | Notes |
|---|----------|----------|---------|-------------|-------|
| 1 | dcg-reviewer | P2 | Missing regression test for ansible safe-pattern shadowing edge case | Incorporated | Added E3 regression cases: `-m shell -a 'rm /tmp/setup'` → Deny/Critical + `-m setup -a 'rm /tmp/data'` → Allow |

## Completion Signoff

- **Date**: 2026-03-03
- **Signed off by**: dcg-coder-2
- **Bead**: dcg-lmc.3
- **Status**: NOT IMPLEMENTED (0% — test harness skeleton exists but all substantive tests skip)

### Summary

Test harness scaffold files exist and are well-structured (`infra_cloud_property_test.go`, `infra_cloud_fault_oracle_bench_security_test.go`), but all pack-dependent tests skip at runtime because none of the 6 infra/cloud packs are registered in `DefaultRegistry`.

### Test Harness File Status

| File | Exists | Functional |
|------|--------|-----------|
| `internal/testharness/infra_cloud_property_test.go` | YES | Skeleton only — all property tests SKIP |
| `internal/testharness/infra_cloud_fault_oracle_bench_security_test.go` | YES | Skeleton only — fault/oracle/security/stress tests SKIP or pass vacuously |

### Exit Criteria Assessment

| # | Criterion | Status |
|---|-----------|--------|
| 1 | All 58 destructive patterns reachable (P1) | **FAIL** — 0 of 58 pass, all 7 pack subtests skip |
| 2 | Safe patterns don't shadow destructive (P2) | **FAIL** — no safe patterns exist |
| 3 | Env sensitivity verified (P3) | **FAIL** — all skip |
| 4 | Auto-approve split validated (P4) | **FAIL** — all skip |
| 5 | ArgAt correctness (P5) | **FAIL** — all skip |
| 6 | 132 golden entries pass (O1) | **FAIL** — 0 golden entries exist |
| 7 | Benchmarks <50µs | **PASS** — <5µs (vacuous) |
| 8 | No panics on malformed input (F1-F2) | **PASS** (vacuous) |
| 9 | Subcommand evasion tested (SEC1) | **FAIL** — all skip |
| 10 | Ansible content injection tested (SEC2) | **FAIL** — skip |
| 11 | Env sensitivity preconditions (SEC3) | **FAIL** — all skip |

**Exit criteria met: 2 of 11 (both vacuously)**

### Gaps

The test harness is designed to activate automatically when packs are registered. No test code changes are needed — only the pack implementations.
