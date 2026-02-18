/*
Copyright 2025 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	admissionv1 "k8s.io/api/admission/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kjson "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-example-driver/internal/profiles"
	"sigs.k8s.io/dra-example-driver/internal/profiles/gpu"
	"sigs.k8s.io/dra-example-driver/pkg/flags"
)

type Flags struct {
	loggingConfig *flags.LoggingConfig

	certFile   string
	keyFile    string
	port       int
	profile    string
	driverName string
}

type validator func(runtime.Object) error

var validProfiles = map[string]profiles.ConfigHandler{
	gpu.ProfileName: gpu.Profile{},
}

func main() {
	if err := newApp().Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newApp() *cli.App {
	flags := &Flags{
		loggingConfig: flags.NewLoggingConfig(),
	}
	cliFlags := []cli.Flag{
		&cli.StringFlag{
			Name:        "tls-cert-file",
			Usage:       "File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated after server cert).",
			Destination: &flags.certFile,
			Required:    true,
		},
		&cli.StringFlag{
			Name:        "tls-private-key-file",
			Usage:       "File containing the default x509 private key matching --tls-cert-file.",
			Destination: &flags.keyFile,
			Required:    true,
		},
		&cli.IntFlag{
			Name:        "port",
			Usage:       "Secure port that the webhook listens on",
			Value:       443,
			Destination: &flags.port,
		},
		&cli.StringFlag{
			Name:        "device-profile",
			Usage:       fmt.Sprintf("Name of the device profile. Valid values are %q.", validProfiles),
			Value:       gpu.ProfileName,
			Destination: &flags.profile,
			EnvVars:     []string{"DEVICE_PROFILE"},
		},
		&cli.StringFlag{
			Name:        "driver-name",
			Usage:       "Name of the DRA driver. Its default is derived from the device profile.",
			Destination: &flags.driverName,
			EnvVars:     []string{"DRIVER_NAME"},
		},
	}
	cliFlags = append(cliFlags, flags.loggingConfig.Flags()...)

	app := &cli.App{
		Name:            "dra-example-webhook",
		Usage:           "dra-example-webhook implements a validating admission webhook complementing a DRA driver plugin.",
		ArgsUsage:       " ",
		HideHelpCommand: true,
		Flags:           cliFlags,
		Before: func(c *cli.Context) error {
			if c.Args().Len() > 0 {
				return fmt.Errorf("arguments not supported: %v", c.Args().Slice())
			}
			return flags.loggingConfig.Apply()
		},
		Action: func(c *cli.Context) error {
			configHandler, ok := validProfiles[flags.profile]
			if !ok {
				var valid []string
				for profileName := range validProfiles {
					valid = append(valid, profileName)
				}
				return fmt.Errorf("invalid device profile %q, valid profiles are %q", flags.profile, valid)
			}

			if flags.driverName == "" {
				flags.driverName = flags.profile + ".example.com"
			}

			mux, err := newMux(configHandler, flags.driverName)
			if err != nil {
				return fmt.Errorf("create HTTP mux: %w", err)
			}

			server := &http.Server{
				Handler: mux,
				Addr:    fmt.Sprintf(":%d", flags.port),
			}
			klog.Background().Info("starting webhook server", "addr", server.Addr)
			return server.ListenAndServeTLS(flags.certFile, flags.keyFile)
		},
	}

	return app
}

func newMux(configHandler profiles.ConfigHandler, driverName string) (*http.ServeMux, error) {
	configScheme := runtime.NewScheme()
	sb := configHandler.SchemeBuilder()
	if err := sb.AddToScheme(configScheme); err != nil {
		return nil, fmt.Errorf("create config scheme: %w", err)
	}
	configDecoder := kjson.NewSerializerWithOptions(
		kjson.DefaultMetaFactory,
		configScheme,
		configScheme,
		kjson.SerializerOptions{
			Pretty: true, Strict: true,
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/validate-resource-claim-parameters", serveResourceClaim(configDecoder, configHandler.Validate, driverName))
	mux.HandleFunc("/readyz", readyHandler)
	return mux, nil
}

func readyHandler(w http.ResponseWriter, req *http.Request) {
	_, err := w.Write([]byte("ok"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func serveResourceClaim(configDecoder runtime.Decoder, validate validator, driverName string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, r.Context(), admitResourceClaimParameters(configDecoder, validate, driverName))
	}
}

// serve handles the http portion of a request prior to handing to an admit
// function.
func serve(w http.ResponseWriter, r *http.Request, ctx context.Context, admit func(context.Context, admissionv1.AdmissionReview) *admissionv1.AdmissionResponse) {
	logger := klog.FromContext(ctx)
	var body []byte
	if r.Body != nil {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Error(err, "failed to read request body")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		body = data
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		msg := fmt.Sprintf("contentType=%s, expected application/json", contentType)
		logger.Error(nil, msg)
		http.Error(w, msg, http.StatusUnsupportedMediaType)
		return
	}

	logger.V(2).Info("handling request", "body", string(body))

	requestedAdmissionReview, err := readAdmissionReview(body)
	if err != nil {
		msg := fmt.Sprintf("failed to read AdmissionReview from request body: %v", err)
		logger.Error(err, msg)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	responseAdmissionReview := &admissionv1.AdmissionReview{}
	responseAdmissionReview.SetGroupVersionKind(requestedAdmissionReview.GroupVersionKind())
	responseAdmissionReview.Response = admit(ctx, *requestedAdmissionReview)
	responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID

	logger.V(2).Info("sending response", "response", responseAdmissionReview)
	respBytes, err := json.Marshal(responseAdmissionReview)
	if err != nil {
		logger.Error(err, "failed to marshal response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error(err, "failed to write response")
	}
}

func readAdmissionReview(data []byte) (*admissionv1.AdmissionReview, error) {
	deserializer := codecs.UniversalDeserializer()
	obj, gvk, err := deserializer.Decode(data, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("request could not be decoded: %w", err)
	}

	if *gvk != admissionv1.SchemeGroupVersion.WithKind("AdmissionReview") {
		return nil, fmt.Errorf("unsupported group version kind: %v", gvk)
	}

	requestedAdmissionReview, ok := obj.(*admissionv1.AdmissionReview)
	if !ok {
		return nil, fmt.Errorf("expected v1.AdmissionReview but got: %T", obj)
	}

	return requestedAdmissionReview, nil
}

// admitResourceClaimParameters accepts both ResourceClaims and ResourceClaimTemplates and validates their
// opaque device configuration parameters for this driver.
func admitResourceClaimParameters(configDecoder runtime.Decoder, validate validator, driverName string) func(context.Context, admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	return func(ctx context.Context, ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
		logger := klog.FromContext(ctx)
		logger.V(2).Info("admitting resource claim parameters")

		var deviceConfigs []resourceapi.DeviceClaimConfiguration
		var specPath string

		switch ar.Request.Resource {
		case resourceClaimResourceV1, resourceClaimResourceV1Beta1, resourceClaimResourceV1Beta2:
			claim, err := extractResourceClaim(ar)
			if err != nil {
				logger.Error(err, "failed to extract ResourceClaim")
				return &admissionv1.AdmissionResponse{
					Result: &metav1.Status{
						Message: err.Error(),
						Reason:  metav1.StatusReasonBadRequest,
					},
				}
			}
			deviceConfigs = claim.Spec.Devices.Config
			specPath = "spec"
		case resourceClaimTemplateResourceV1, resourceClaimTemplateResourceV1Beta1, resourceClaimTemplateResourceV1Beta2:
			claimTemplate, err := extractResourceClaimTemplate(ar)
			if err != nil {
				logger.Error(err, "failed to extract ResourceClaimTemplate")
				return &admissionv1.AdmissionResponse{
					Result: &metav1.Status{
						Message: err.Error(),
						Reason:  metav1.StatusReasonBadRequest,
					},
				}
			}
			deviceConfigs = claimTemplate.Spec.Spec.Devices.Config
			specPath = "spec.spec"
		default:
			expected := []metav1.GroupVersionResource{
				resourceClaimResourceV1, resourceClaimResourceV1Beta1, resourceClaimResourceV1Beta2,
				resourceClaimTemplateResourceV1, resourceClaimTemplateResourceV1Beta1, resourceClaimTemplateResourceV1Beta2,
			}
			msg := fmt.Sprintf("expected resource to be one of %v, got %s", expected, ar.Request.Resource)
			logger.Error(nil, msg)
			return &admissionv1.AdmissionResponse{
				Result: &metav1.Status{
					Message: msg,
					Reason:  metav1.StatusReasonBadRequest,
				},
			}
		}

		var errs []error
		for configIndex, config := range deviceConfigs {
			if config.Opaque == nil || config.Opaque.Driver != driverName {
				continue
			}

			fieldPath := fmt.Sprintf("%s.devices.config[%d].opaque.parameters", specPath, configIndex)
			decodedConfig, err := runtime.Decode(configDecoder, config.Opaque.Parameters.Raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("error decoding object at %s: %w", fieldPath, err))
				continue
			}
			err = validate(decodedConfig)
			if err != nil {
				errs = append(errs, fmt.Errorf("object at %s is invalid: %w", fieldPath, err))
			}
		}

		if len(errs) > 0 {
			var errMsgs []string
			for _, err := range errs {
				errMsgs = append(errMsgs, err.Error())
			}
			msg := fmt.Sprintf("%d configs failed to validate: %s", len(errs), strings.Join(errMsgs, "; "))
			logger.Error(nil, msg)
			return &admissionv1.AdmissionResponse{
				Result: &metav1.Status{
					Message: msg,
					Reason:  metav1.StatusReason(metav1.StatusReasonInvalid),
				},
			}
		}

		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}
}
