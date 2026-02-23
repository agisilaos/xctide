package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func handleCommandMode(commandMode string, cfg *buildConfig) (handled bool, exitCode int) {
	switch commandMode {
	case "doctor":
		result := runDoctor(*cfg)
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				return true, exitRuntimeFailure
			}
		} else {
			renderDoctorResult(os.Stdout, result)
		}
		if result.Success {
			return true, exitOK
		}
		return true, exitRuntimeFailure
	case "diagnose_build":
		result := runDiagnoseBuild(*cfg)
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				return true, exitRuntimeFailure
			}
		} else {
			renderDiagnoseBuildResult(os.Stdout, result)
		}
		if result.Ready {
			return true, exitOK
		}
		return true, exitRuntimeFailure
	case "plan":
		if err := autoDetectConfig(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			return true, exitConfigFailure
		}
		result := buildPlanResult(*cfg, commandMode)
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				return true, exitRuntimeFailure
			}
		} else {
			renderPlanResult(os.Stdout, result)
		}
		return true, exitOK
	case "destinations":
		if err := autoDetectConfig(cfg); err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			return true, exitConfigFailure
		}
		options, err := listDestinations(*cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "xctide:", err)
			return true, exitRuntimeFailure
		}
		result := destinationsResult{
			Project:      cfg.projectPath,
			Workspace:    cfg.workspacePath,
			Scheme:       cfg.scheme,
			Destinations: options,
		}
		if cfg.jsonOutput {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				fmt.Fprintln(os.Stderr, "xctide:", err)
				return true, exitRuntimeFailure
			}
		} else {
			renderDestinationsResult(os.Stdout, result, *cfg)
		}
		return true, exitOK
	default:
		return false, exitOK
	}
}
