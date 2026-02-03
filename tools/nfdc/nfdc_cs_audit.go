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
	}.Append(prefix...).
		// 中文说明：追加一个 "_"，避免 forwarder 生成 status dataset 时的 WithVersion() 把 prefix 末尾的版本组件误覆盖。
		Append(enc.NewGenericComponent("_"))

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
	}.Append(name...).
		// 中文说明：追加一个 "_"，避免 forwarder 生成 status dataset 时的 WithVersion() 把“目标 name 的版本组件”误覆盖。
		Append(enc.NewGenericComponent("_"))

	data, err := t.fetchStatusDataset(suffix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching cs-audit leaf: %+v\n", err)
		os.Exit(1)
		return
	}

	fmt.Printf("%s\n", hex.EncodeToString(data.Join()))
}

// cs-audit-flip /name
func (t *Tool) ExecCsAuditFlip(_ *cobra.Command, args []string) {
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
		enc.NewGenericComponent("flip"),
	}.Append(name...).
		// 中文说明：追加一个 "_"，避免 forwarder 生成 status dataset 时的 WithVersion() 把“目标 name 的版本组件”误覆盖。
		Append(enc.NewGenericComponent("_"))

	data, err := t.fetchStatusDataset(suffix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing cs-audit flip: %+v\n", err)
		os.Exit(1)
		return
	}

	// 中文说明：flip 返回的是一段可读字符串，直接打印即可。
	fmt.Printf("%s\n", string(data.Join()))
}
