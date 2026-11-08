// device-profile manages cloud-local presentation names. It does not change or
// claim to read the firmware's acoustic wake word.
package main

import (
	"encoding/json"
	"familyrobot/internal/deviceprofile"
	"familyrobot/internal/realtimestate"
	"flag"
	"fmt"
	"io"
	"os"
)

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("device-profile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("profiles", "", "private device profile JSON path")
	defaultName := flags.String("default-name", "", "optional fallback name override")
	identity := flags.String("identity", "config/product-identity.json", "read-only product facts for the get fallback")
	if flags.Parse(args) != nil {
		return 2
	}
	argv := flags.Args()
	if *path == "" || len(argv) < 2 || (argv[0] != "get" && argv[0] != "set") || (argv[0] == "get" && len(argv) != 2) || (argv[0] == "set" && len(argv) != 3) {
		fmt.Fprintln(stderr, "usage: device-profile --profiles PATH [--identity PATH | --default-name NAME] get DEVICE_ID | set DEVICE_ID NAME")
		return 2
	}
	fallback := ""
	if argv[0] == "get" {
		if *defaultName != "" {
			var err error
			fallback, err = deviceprofile.NormalizeName(*defaultName)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 2
			}
		} else {
			product, err := realtimestate.LoadProduct(*identity)
			if err != nil {
				fmt.Fprintln(stderr, "product_identity_invalid")
				return 1
			}
			fallback = product.Name()
		}
	}
	store, err := deviceprofile.Open(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var profile deviceprofile.Profile
	if argv[0] == "set" {
		profile, err = store.SetName(argv[1], argv[2])
	} else {
		profile, err = store.Resolve(argv[1], fallback)
		if err == deviceprofile.ErrNotFound || err == deviceprofile.ErrUnavailable {
			fmt.Fprintln(stderr, err)
			err = nil
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	output := struct {
		Name     string `json:"name"`
		Revision uint64 `json:"revision"`
		Wake     string `json:"effective_wake_word"`
	}{profile.Name, profile.Revision, "unknown"}
	if json.NewEncoder(stdout).Encode(output) != nil {
		return 1
	}
	return 0
}
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
