package main

import (
	"context"
	"flag"
	"fmt"

	"github.com/Frantche/homelab-admin-node/internal/validate"
)

func (a app) runValidate(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "all")
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	output := fs.String("output", "text", "output format: text or json")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if !a.requireOperationalConfig() {
		return 1
	}

	validator := validate.NewValidator(a.cfg, a.runner)
	var results []validate.CheckResult
	switch subcommand {
	case "all":
		results = validator.All(ctx)
	case "apis":
		results = validator.APIS(ctx)
	case "harbor":
		results = []validate.CheckResult{validator.Harbor(ctx)}
	case "openbao":
		results = []validate.CheckResult{validator.OpenBao(ctx)}
	case "gitea":
		results = []validate.CheckResult{validator.Gitea(ctx)}
	case "dns":
		results = []validate.CheckResult{validator.DNS(ctx)}
	case "tunnel":
		results = []validate.CheckResult{validator.Tunnel(ctx)}
	case "hardening":
		results = []validate.CheckResult{validator.Hardening(ctx)}
	case "observability":
		results = []validate.CheckResult{validator.Observability(ctx)}
	default:
		fmt.Fprintf(a.errOut, "unknown validate command: %s\n", subcommand)
		return 2
	}
	return a.writeValidationResults(results, *output)
}

func (a app) runTest(ctx context.Context, args []string) int {
	subcommand, rest := splitSubcommand(args, "")
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(a.errOut)
	output := fs.String("output", "text", "output format: text or json")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if !a.requireOperationalConfig() {
		return 1
	}

	validator := validate.NewValidator(a.cfg, a.runner)
	var results []validate.CheckResult
	switch subcommand {
	case "harbor-scanner":
		results = []validate.CheckResult{validator.HarborScanner(ctx)}
	default:
		fmt.Fprintf(a.errOut, "unknown test command: %s\n", subcommand)
		return 2
	}
	return a.writeValidationResults(results, *output)
}

func (a app) writeValidationResults(results []validate.CheckResult, output string) int {
	switch output {
	case "text":
		validate.WriteText(a.out, results)
	case "json":
		if err := validate.WriteJSON(a.out, results); err != nil {
			fmt.Fprintf(a.errOut, "write json output: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(a.errOut, "unknown output format: %s\n", output)
		return 2
	}
	if validate.HasFailure(results) {
		return 1
	}
	return 0
}
