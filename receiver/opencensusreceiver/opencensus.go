// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package opencensusreceiver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	agentmetricspb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/metrics/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	gatewayruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/rs/cors"
	"github.com/soheilhy/cmux"
	"google.golang.org/grpc"

	"github.com/open-telemetry/opentelemetry-collector/component"
	"github.com/open-telemetry/opentelemetry-collector/consumer"
	"github.com/open-telemetry/opentelemetry-collector/observability"
	"github.com/open-telemetry/opentelemetry-collector/oterr"
	"github.com/open-telemetry/opentelemetry-collector/receiver/opencensusreceiver/ocmetrics"
	"github.com/open-telemetry/opentelemetry-collector/receiver/opencensusreceiver/octrace"
)

// Receiver is the type that exposes Trace and Metrics reception.
type Receiver struct {
	mu                sync.Mutex
	ln                net.Listener
	serverGRPC        *grpc.Server
	serverHTTP        *http.Server
	gatewayMux        *gatewayruntime.ServeMux
	corsOrigins       []string
	grpcServerOptions []grpc.ServerOption

	traceReceiverOpts []octrace.Option

	traceReceiver   *octrace.Receiver
	metricsReceiver *ocmetrics.Receiver

	traceConsumer   consumer.TraceConsumerOld
	metricsConsumer consumer.MetricsConsumerOld

	stopOnce                 sync.Once
	startServerOnce          sync.Once
	startTraceReceiverOnce   sync.Once
	startMetricsReceiverOnce sync.Once

	instanceName string
}

// New just creates the OpenCensus receiver services. It is the caller's
// responsibility to invoke the respective Start*Reception methods as well
// as the various Stop*Reception methods to end it.
func New(
	instanceName string,
	transport string,
	addr string,
	tc consumer.TraceConsumerOld,
	mc consumer.MetricsConsumerOld,
	opts ...Option,
) (*Receiver, error) {
	// TODO: (@odeke-em) use options to enable address binding changes.
	ln, err := net.Listen(transport, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind to address %q: %v", addr, err)
	}

	ocr := &Receiver{
		ln:          ln,
		corsOrigins: []string{}, // Disable CORS by default.
		gatewayMux:  gatewayruntime.NewServeMux(),
	}

	for _, opt := range opts {
		opt.withReceiver(ocr)
	}

	ocr.instanceName = instanceName
	ocr.traceConsumer = tc
	ocr.metricsConsumer = mc

	return ocr, nil
}

// Start runs the trace receiver on the gRPC server. Currently
// it also enables the metrics receiver too.
func (ocr *Receiver) Start(host component.Host) error {
	return ocr.start(host)
}

func (ocr *Receiver) registerTraceConsumer() error {
	var err = oterr.ErrAlreadyStarted

	ocr.startTraceReceiverOnce.Do(func() {
		ocr.traceReceiver, err = octrace.New(
			ocr.instanceName, ocr.traceConsumer, ocr.traceReceiverOpts...)
		if err == nil {
			srv := ocr.grpcServer()
			agenttracepb.RegisterTraceServiceServer(srv, ocr.traceReceiver)
		}
	})

	return err
}

func (ocr *Receiver) registerMetricsConsumer() error {
	var err = oterr.ErrAlreadyStarted

	ocr.startMetricsReceiverOnce.Do(func() {
		ocr.metricsReceiver, err = ocmetrics.New(
			ocr.instanceName, ocr.metricsConsumer)
		if err == nil {
			srv := ocr.grpcServer()
			agentmetricspb.RegisterMetricsServiceServer(srv, ocr.metricsReceiver)
		}
	})
	return err
}

func (ocr *Receiver) grpcServer() *grpc.Server {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	if ocr.serverGRPC == nil {
		ocr.serverGRPC = observability.GRPCServerWithObservabilityEnabled(ocr.grpcServerOptions...)
	}

	return ocr.serverGRPC
}

// Shutdown is a method to turn off receiving.
func (ocr *Receiver) Shutdown() error {
	if err := ocr.stop(); err != oterr.ErrAlreadyStopped {
		return err
	}
	return nil
}

// start runs all the receivers/services namely, Trace and Metrics services.
func (ocr *Receiver) start(host component.Host) error {
	hasConsumer := false
	if ocr.traceConsumer != nil {
		hasConsumer = true
		if err := ocr.registerTraceConsumer(); err != nil && err != oterr.ErrAlreadyStarted {
			return err
		}
	}

	if ocr.metricsConsumer != nil {
		hasConsumer = true
		if err := ocr.registerMetricsConsumer(); err != nil && err != oterr.ErrAlreadyStarted {
			return err
		}
	}

	if !hasConsumer {
		return errors.New("cannot start receiver: no consumers were specified")
	}

	if err := ocr.startServer(host); err != nil && err != oterr.ErrAlreadyStarted {
		return err
	}

	// At this point we've successfully started all the services/receivers.
	// Add other start routines here.
	return nil
}

// stop stops the underlying gRPC server and all the services running on it.
func (ocr *Receiver) stop() error {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	var err = oterr.ErrAlreadyStopped
	ocr.stopOnce.Do(func() {
		err = nil

		if ocr.serverHTTP != nil {
			_ = ocr.serverHTTP.Close()
		}

		if ocr.ln != nil {
			_ = ocr.ln.Close()
		}

		// TODO: @(odeke-em) investigate what utility invoking (*grpc.Server).Stop()
		// gives us yet we invoke (net.Listener).Close().
		// Sure (*grpc.Server).Stop() enables proper shutdown but imposes
		// a painful and artificial wait time that goes into 20+seconds yet most of our
		// tests and code should be reactive in less than even 1second.
		// ocr.serverGRPC.Stop()
	})
	return err
}

func (ocr *Receiver) httpServer() *http.Server {
	ocr.mu.Lock()
	defer ocr.mu.Unlock()

	if ocr.serverHTTP == nil {
		var mux http.Handler = ocr.gatewayMux
		if len(ocr.corsOrigins) > 0 {
			co := cors.Options{AllowedOrigins: ocr.corsOrigins}
			mux = cors.New(co).Handler(mux)
		}
		ocr.serverHTTP = &http.Server{Handler: mux}
	}

	return ocr.serverHTTP
}

func (ocr *Receiver) startServer(host component.Host) error {
	err := oterr.ErrAlreadyStarted
	ocr.startServerOnce.Do(func() {
		err = nil
		// Register the grpc-gateway on the HTTP server mux
		c := context.Background()
		opts := []grpc.DialOption{grpc.WithInsecure()}
		endpoint := ocr.ln.Addr().String()

		_, ok := ocr.ln.(*net.UnixListener)
		if ok {
			endpoint = "unix:" + endpoint
		}

		err = agenttracepb.RegisterTraceServiceHandlerFromEndpoint(c, ocr.gatewayMux, endpoint, opts)
		if err != nil {
			return
		}

		err = agentmetricspb.RegisterMetricsServiceHandlerFromEndpoint(c, ocr.gatewayMux, endpoint, opts)
		if err != nil {
			return
		}

		// Start the gRPC and HTTP/JSON (grpc-gateway) servers on the same port.
		m := cmux.New(ocr.ln)
		grpcL := m.MatchWithWriters(
			cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"),
			cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc+proto"))

		httpL := m.Match(cmux.Any())
		go func() {
			if err := ocr.serverGRPC.Serve(grpcL); err != nil {
				host.ReportFatalError(err)
			}
		}()
		go func() {
			if err := ocr.httpServer().Serve(httpL); err != nil {
				host.ReportFatalError(err)
			}
		}()
		go func() {
			if err := m.Serve(); err != nil {
				host.ReportFatalError(err)
			}
		}()
	})
	return err
}
