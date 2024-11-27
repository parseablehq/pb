// Copyright (c) 2024 Parseable, Inc
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

// import (
// 	"fmt"
// 	"log"
// 	"os"
// 	"sync"
// 	"time"

// 	internalHTTP "pb/pkg/http"

// 	"github.com/briandowns/spinner"

// 	"pb/pkg/analyze/anthropic"
// 	"pb/pkg/analyze/duckdb"
// 	"pb/pkg/analyze/k8s"
// 	"pb/pkg/analyze/ollama"
// 	"pb/pkg/analyze/openai"

// 	_ "github.com/marcboeker/go-duckdb"
// 	"github.com/spf13/cobra"
// )

// var GenerateCmd = &cobra.Command{
// 	Use:     "k8s",
// 	Short:   "Generate k8s events on your k8s cluster by deploying apps in different states.",
// 	Example: "pb generate k8s events",
// 	Args:    cobra.ExactArgs(1),
// 	RunE: func(cmd *cobra.Command, args []string) error {
// 	}
// }
