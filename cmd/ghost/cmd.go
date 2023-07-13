//  Copyright (C) 2021-2023 Chronicle Labs, Inc.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"github.com/spf13/cobra"

	"github.com/chronicleprotocol/oracle-suite/cmd"
	"github.com/chronicleprotocol/oracle-suite/pkg/config/ghost"
	"github.com/chronicleprotocol/oracle-suite/pkg/config/ghostnext"
)

type options struct {
	cmd.LoggerFlags
	cmd.FilesFlags
	Config     ghost.Config
	Config2    ghostnext.Config
	GoferNoRPC bool
}

func NewRootCommand(opts *options) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "ghost",
		Version:       cmd.Version,
		SilenceErrors: false,
		SilenceUsage:  true,
	}

	rootCmd.PersistentFlags().AddFlagSet(cmd.NewLoggerFlagSet(&opts.LoggerFlags))
	rootCmd.PersistentFlags().AddFlagSet(cmd.NewFilesFlagSet(&opts.FilesFlags))
	rootCmd.PersistentFlags().BoolVar(
		&opts.GoferNoRPC,
		"gofer.norpc",
		false,
		"disable the use of Graph RPC agent",
	)

	return rootCmd
}
