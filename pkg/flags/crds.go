/*
 * Copyright 2023 The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package flags

import (
	"github.com/urfave/cli/v2"
)

type CRDConfig struct {
	NodeName  string
	Namespace string

	HideNodeName bool
}

func (n *CRDConfig) Flags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "namespace",
			Usage:       "The namespace used for the custom resources.",
			Value:       "default",
			Destination: &n.Namespace,
			EnvVars:     []string{"NAMESPACE"},
		},
	}
	if !n.HideNodeName {
		flags = append(flags, &cli.StringFlag{
			Name:        "node-name",
			Usage:       "The name of the node to be worked on.",
			Required:    true,
			Destination: &n.NodeName,
			EnvVars:     []string{"NODE_NAME"},
		})
	}

	return flags
}
