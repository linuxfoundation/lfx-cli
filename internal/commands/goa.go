// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

// Package commands implements the lfx CLI subcommands.
package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strings"

	"github.com/urfave/cli/v3"
	goahttp "goa.design/goa/v3/http"
	goa "goa.design/goa/v3/pkg"
)

// goaArgKind selects how a urfave flag value is converted into the string
// argument a Goa-generated Build<Method>Payload function expects.
type goaArgKind int

const (
	// goaArgString passes a string flag's value through verbatim. Goa's
	// builder treats "" as unset and parses non-string types (such as
	// integers) itself, so numeric parameters also use this kind.
	goaArgString goaArgKind = iota
	// goaArgJSONList encodes a repeatable string flag as the JSON array
	// string Goa's builder unmarshals, or "" when no values were given.
	goaArgJSONList
	// goaArgBearerToken passes the access token resolved from `lfx auth`;
	// it has no flag.
	goaArgBearerToken
)

// goaBuilderArg describes one positional parameter of a Goa-generated
// Build<Method>Payload function. A goaEndpointSpec's Args must list every
// builder parameter in signature order; Param must equal the generated
// parameter name, which TestGoaBuilderSignatures checks against the
// generated source so that a reordered, renamed, added, or removed
// parameter fails the test rather than silently misrouting a flag value.
type goaBuilderArg struct {
	Param   string
	Kind    goaArgKind
	Flag    string
	Usage   string
	Default string
}

// goaService describes how to reach one Goa-generated LFX v2 service.
type goaService[C any] struct {
	// Name is the command name for the service, e.g. "query-svc".
	Name string
	// Usage is the help text for the service's command group.
	Usage string
	// NewClient builds the generated HTTP client for the service.
	NewClient func(scheme, host string, doer goahttp.Doer) C
	// Examples are common invocations shown in the service command's help.
	// They are kept separate from the generated endpoint specs so that
	// regenerating or reconciling specs never touches them.
	Examples []goaExample
}

// goaExample is one example invocation shown in a command's help.
type goaExample struct {
	// Description says what the example does.
	Description string
	// Command is the full invocation, starting with "lfx".
	Command string
}

// renderGoaUsageText formats a command's usage line followed by its
// examples. Examples go in UsageText rather than Description because
// cli-docs renders a subcommand's UsageText (as a code block) but omits
// its Description, and `lfx docs` output is meant for agents.
func renderGoaUsageText(usage string, examples []goaExample) string {
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(usage)
	b.WriteString("\n\nExamples:\n")
	for i, ex := range examples {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "  # %s\n  %s\n", ex.Description, ex.Command)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// goaEndpointSpec describes one generated endpoint exposed as an lfx
// subcommand.
type goaEndpointSpec[C any] struct {
	Name  string
	Usage string
	// Builder is the generated Build<Method>Payload function.
	Builder any
	// Args lists Builder's parameters in signature order.
	Args []goaBuilderArg
	// Endpoint selects the endpoint from the generated HTTP client,
	// typically a method expression such as (*client.Client).QueryOrgs.
	Endpoint func(C) goa.Endpoint
}

// command builds the urfave command group for the service from specs.
func (s goaService[C]) command(specs []goaEndpointSpec[C]) *cli.Command {
	commands := make([]*cli.Command, 0, len(specs))
	for _, spec := range specs {
		commands = append(commands, s.endpointCommand(spec))
	}
	return &cli.Command{
		Name:      s.Name,
		Usage:     s.Usage,
		UsageText: renderGoaUsageText("lfx "+s.Name+" <command> [options]", s.Examples),
		Commands:  commands,
	}
}

// goaOutputFlags returns the output flags every Goa-backed endpoint
// command has, matching `lfx api`'s. Their names and aliases are listed in
// goaReservedFlagNames.
func goaOutputFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    apiQueryFlagName,
			Aliases: []string{"q"},
			Usage:   "Filter the response body through a gjson expression before output",
		},
	}
}

// goaReservedFlagNames lists flag names and aliases that endpoint
// arguments must not use: the output flags from goaOutputFlags and
// urfave's built-in help flag. TestGoaBuilderSignatures enforces this; a
// colliding service parameter gets a different flag name in its spec.
var goaReservedFlagNames = []string{apiQueryFlagName, "q", "help", "h"}

// endpointCommand builds the urfave command for a single endpoint, with
// one flag per non-token builder argument plus the output flags.
func (s goaService[C]) endpointCommand(spec goaEndpointSpec[C]) *cli.Command {
	var flags []cli.Flag
	for _, arg := range spec.Args {
		switch arg.Kind {
		case goaArgString:
			flags = append(flags, &cli.StringFlag{Name: arg.Flag, Usage: arg.Usage, Value: arg.Default})
		case goaArgJSONList:
			flags = append(flags, &cli.StringSliceFlag{Name: arg.Flag, Usage: arg.Usage + " (repeatable)"})
		case goaArgBearerToken:
		default:
			panic(fmt.Sprintf("unsupported argument kind %d for %s", arg.Kind, arg.Param))
		}
	}
	flags = append(flags, goaOutputFlags()...)
	return &cli.Command{
		Name:  spec.Name,
		Usage: spec.Usage,
		Flags: flags,
		// Deliver each repeatable flag occurrence as one element rather
		// than splitting on ",", which filter and tag values may contain.
		DisableSliceFlagSeparator: true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return s.run(ctx, cmd, spec)
		},
	}
}

// run resolves credentials and the service URL, builds the payload with
// the generated builder, calls the endpoint, and writes the raw response
// body to stdout.
func (s goaService[C]) run(ctx context.Context, cmd *cli.Command, spec goaEndpointSpec[C]) error {
	token, baseURL, err := resolveAuthenticatedBaseURL(ctx, cmd)
	if err != nil {
		return err
	}
	// The generated client's endpoint already emits its full request path
	// (e.g. QueryResourcesQuerySvcPath returns "/query/resources"), so
	// only baseURL's scheme and host are passed to NewClient.
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL %q: %w", baseURL, err)
	}

	values, err := goaBuilderValues(cmd, spec.Args, token)
	if err != nil {
		return err
	}
	payload, err := callGoaBuilder(spec.Builder, values)
	if err != nil {
		return err
	}

	doer := &rawBodyDoer{inner: newNoRedirectHTTPClient()}
	endpoint := spec.Endpoint(s.NewClient(parsed.Scheme, parsed.Host, doer))
	if _, err := endpoint(ctx, payload); err != nil {
		return formatGoaError(err)
	}

	if query := cmd.String(apiQueryFlagName); query != "" {
		return writeQueryOutput(doer.body, query)
	}
	if _, err := os.Stdout.Write(doer.body); err != nil {
		return fmt.Errorf("write response body: %w", err)
	}
	return nil
}

// goaBuilderValues converts the command's flags into the positional string
// arguments for a generated builder, in args order.
func goaBuilderValues(cmd *cli.Command, args []goaBuilderArg, token string) ([]string, error) {
	values := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg.Kind {
		case goaArgString:
			values = append(values, cmd.String(arg.Flag))
		case goaArgJSONList:
			list := cmd.StringSlice(arg.Flag)
			if len(list) == 0 {
				values = append(values, "")
				continue
			}
			encoded, err := json.Marshal(list)
			if err != nil {
				return nil, fmt.Errorf("encode --%s: %w", arg.Flag, err)
			}
			values = append(values, string(encoded))
		case goaArgBearerToken:
			values = append(values, token)
		default:
			return nil, fmt.Errorf("unsupported argument kind %d for %s", arg.Kind, arg.Param)
		}
	}
	return values, nil
}

// callGoaBuilder invokes a generated Build<Method>Payload function, whose
// parameters are all strings and whose results are (payload, error).
func callGoaBuilder(builder any, values []string) (any, error) {
	fn := reflect.ValueOf(builder)
	t := fn.Type()
	if t.Kind() != reflect.Func || t.NumOut() != 2 || !t.Out(1).Implements(reflect.TypeFor[error]()) {
		return nil, fmt.Errorf("%s is not a Goa payload builder", goaFuncName(builder))
	}
	if t.NumIn() != len(values) {
		return nil, fmt.Errorf("%s takes %d arguments, but %d were mapped", goaFuncName(builder), t.NumIn(), len(values))
	}
	in := make([]reflect.Value, len(values))
	for i, v := range values {
		if t.In(i).Kind() != reflect.String {
			return nil, fmt.Errorf("%s argument %d is not a string", goaFuncName(builder), i)
		}
		in[i] = reflect.ValueOf(v)
	}
	out := fn.Call(in)
	if err, _ := out[1].Interface().(error); err != nil {
		return nil, err
	}
	return out[0].Interface(), nil
}

// goaFuncName returns the fully qualified name of a function value, e.g.
// "github.com/org/repo/gen/http/svc/client.BuildFooPayload".
func goaFuncName(fn any) string {
	return runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
}

// rawBodyDoer wraps a goahttp.Doer and keeps a copy of the last response
// body, so the command can print the service's JSON exactly as returned
// while the generated client still decodes and validates it.
type rawBodyDoer struct {
	inner goahttp.Doer
	body  []byte
}

// maxGoaResponseBodyBytes caps how much of a response body rawBodyDoer
// buffers in memory, since the generated client has no size limit of its
// own and a malicious or misbehaving service could otherwise exhaust
// memory.
const maxGoaResponseBodyBytes = 64 << 20 // 64 MiB

// Do implements goahttp.Doer.
func (d *rawBodyDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.inner.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := readLimitedBody(resp.Body, maxGoaResponseBodyBytes)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	d.body = body
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// goaNamedError matches the typed errors Goa generates for designed error
// responses, whose Error method returns "" and whose detail is in a
// Message field.
type goaNamedError interface {
	error
	GoaErrorName() string
}

// formatGoaError renders an endpoint error as readable text, filling in the
// name and message of Goa's typed errors.
func formatGoaError(err error) error {
	var named goaNamedError
	if !errors.As(err, &named) || named.Error() != "" {
		return err
	}
	v := reflect.Indirect(reflect.ValueOf(named))
	if v.Kind() == reflect.Struct {
		if msg := v.FieldByName("Message"); msg.IsValid() && msg.Kind() == reflect.String && msg.String() != "" {
			return fmt.Errorf("%s: %s", named.GoaErrorName(), msg.String())
		}
	}
	return errors.New(named.GoaErrorName())
}
