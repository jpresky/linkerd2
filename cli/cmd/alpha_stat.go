package cmd

import (
	"bytes"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/linkerd/linkerd2/controller/api/util"
	"github.com/linkerd/linkerd2/pkg/k8s"
	"github.com/linkerd/linkerd2/pkg/smimetrics"
	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/api/resource"
)

type alphaStatOptions struct {
	namespace string
}

func newCmdAlphaStat() *cobra.Command {
	options := alphaStatOptions{
		namespace: "default",
	}

	statCmd := &cobra.Command{
		Use:   "stat [flags] (RESOURCE)",
		Short: "Display traffic stats about one or many resources",
		Long: `Display traffic stats about one or many resources
		
(RESOURCE) can be a resource kind; one of:
  * cronjobs
  * daemonsets
  * deployments
  * jobs
  * pods
  * replicasets
  * replicationcontrollers
  * statefulsets
  * trafficsplits
or it may be a specific named resource of one of the above kinds.

linkerd alpha stat will return a table of the requested resource or resources
showing the top-line metrics for those resources such as request rate, success
rate, and latency percentiles.  These values are measured on the server-side
unless the --to flag is specified.

The --to flag accepts a resource kind or a specific resource and instead
displays the metrics measured on the client side from the root resource to
the to-resource.  At least one of the root resource or the to-resource must be
a specific named resource.  The --to flag is incompatible with a trafficsplit
root resource.

Examples:
  # Topline Resource Metrics
  linkerd alpha stat -n emojivoto deploy/web

  # Topline Resource Metrics for a whole Kind
  linkerd alpha stat -n emojivoto deploy

  # Outbound edges
  linkerd alpha stat -n emojivoto deploy/web --to=deploy

  # Outbound to a specific destination
  linkerd alpha stat -n emojivoto deploy/web --to=deploy/emoji

  # Who calls web?
  linkerd alpha stat -n emojivoto deploy --to deploy/web

  # Traffic splits
  linkerd alpha stat -n emojivoto ts

  # How is web's traffic split?
  linkerd alpha stat -n emojivoto deploy/web --to=ts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k8sAPI, err := k8s.NewAPI(kubeconfigPath, kubeContext, impersonate, impersonateGroup, 0)
			if err != nil {
				return err
			}

			target, err := util.BuildResource(options.namespace, args[0])
			if err != nil {
				return err
			}
			kind, err := k8s.PluralResourceNameFromFriendlyName(target.GetType())
			if err != nil {
				return err
			}

			name := target.GetName()
			var buffer bytes.Buffer
			w := tabwriter.NewWriter(&buffer, 0, 0, padding, ' ', tabwriter.AlignRight)
			if name != "" {
				metrics, err := smimetrics.GetTrafficMetrics(k8sAPI, target.GetNamespace(), kind, name, false, nil)
				if err != nil {
					return err
				}
				renderTrafficMetrics(metrics, w, options)
			} else {
				metrics, err := smimetrics.GetTrafficMetricsList(k8sAPI, target.GetNamespace(), kind, false, nil)
				if err != nil {
					return err
				}
				renderTrafficMetricsList(metrics, w, options)
			}

			w.Flush()
			fmt.Print(buffer.String())
			return nil
		},
	}

	statCmd.PersistentFlags().StringVarP(&options.namespace, "namespace", "n", options.namespace, "Namespace of the specified resource")

	return statCmd
}

func renderTrafficMetrics(metrics *smimetrics.TrafficMetrics, w *tabwriter.Writer, options alphaStatOptions) {
	renderTrafficHeaders(w)
	for _, col := range metricsToRow(metrics) {
		fmt.Fprint(w, col)
		fmt.Fprint(w, "\t")
	}
	fmt.Fprint(w, "\n")
}

func renderTrafficMetricsList(metrics *smimetrics.TrafficMetricsList, w *tabwriter.Writer, options alphaStatOptions) {
	renderTrafficHeaders(w)
	for _, row := range metrics.Items {
		for _, col := range metricsToRow(&row) {
			fmt.Fprint(w, col)
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, "\n")
	}
}

func renderTrafficHeaders(w *tabwriter.Writer) {
	headers := []string{
		"NAME",
		"SUCCESS",
		"RPS",
		"LATENCY_P50",
		"LATENCY_P90",
		"LATENCY_P99",
	}
	for _, header := range headers {
		fmt.Fprint(w, header)
		fmt.Fprint(w, "\t")
	}
	fmt.Fprint(w, "\n")
}

func getNumericMetric(metrics *smimetrics.TrafficMetrics, name string) *resource.Quantity {
	for _, m := range metrics.Metrics {
		if m.Name == name {
			quantity, err := resource.ParseQuantity(m.Value)
			if err != nil {
				return resource.NewQuantity(0, resource.DecimalSI)
			}
			return &quantity
		}
	}
	return resource.NewQuantity(0, resource.DecimalSI)
}

func getNumericMetricWithUnit(metrics *smimetrics.TrafficMetrics, name string) string {
	for _, m := range metrics.Metrics {
		if m.Name == name {
			quantity, err := resource.ParseQuantity(m.Value)
			if err != nil {
				return ""
			}
			value := quantity.Value()
			return fmt.Sprintf("%d%s", value, m.Unit)
		}
	}
	return ""
}

func metricsToRow(metrics *smimetrics.TrafficMetrics) []string {
	success := getNumericMetric(metrics, "success_count").MilliValue()
	failure := getNumericMetric(metrics, "failure_count").MilliValue()
	sr := "-"
	if success+failure > 0 {
		rate := float32(success) / float32(success+failure) * 100
		sr = fmt.Sprintf("%.2f%%", rate)
	}
	rps := "-"
	window, err := time.ParseDuration(metrics.Window)
	if err == nil {
		rate := float64(success+failure) / 1000.0 / window.Seconds()
		rps = fmt.Sprintf("%.1frps", rate)
	}
	return []string{
		metrics.Resource.Name,
		sr,
		rps,
		getNumericMetricWithUnit(metrics, "p50_response_latency"),
		getNumericMetricWithUnit(metrics, "p90_response_latency"),
		getNumericMetricWithUnit(metrics, "p99_response_latency"),
	}
}
