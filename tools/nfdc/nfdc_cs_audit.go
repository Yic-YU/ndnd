package nfdc

import (
	"encoding/hex"
	"fmt"
	"os"

	enc "github.com/named-data/ndnd/std/encoding"
	"github.com/spf13/cobra"
)

// cs-audit-agg [/prefix]
func (t *Tool) ExecCsAuditAgg(_ *cobra.Command, args []string) {
	t.Start()
	defer t.Stop()

	var prefix enc.Name
	if len(args) == 1 {
		var err error
		prefix, err = enc.NameFromStr(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid prefix: %+v\n", err)
			os.Exit(1)
			return
		}
	} else {
		prefix = enc.Name{}
	}

	suffix := enc.Name{
		enc.NewGenericComponent("cs-audit"),
		enc.NewGenericComponent("agg"),
	}.Append(prefix...)

	data, err := t.fetchStatusDataset(suffix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching cs-audit agg: %+v\n", err)
		os.Exit(1)
		return
	}

	fmt.Printf("%s\n", hex.EncodeToString(data.Join()))
}

// cs-audit-leaf /name
func (t *Tool) ExecCsAuditLeaf(_ *cobra.Command, args []string) {
	t.Start()
	defer t.Stop()

	name, err := enc.NameFromStr(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid name: %+v\n", err)
		os.Exit(1)
		return
	}

	suffix := enc.Name{
		enc.NewGenericComponent("cs-audit"),
		enc.NewGenericComponent("leaf"),
	}.Append(name...)

	data, err := t.fetchStatusDataset(suffix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching cs-audit leaf: %+v\n", err)
		os.Exit(1)
		return
	}

	fmt.Printf("%s\n", hex.EncodeToString(data.Join()))
}
