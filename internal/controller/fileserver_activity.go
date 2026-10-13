/*
Copyright 2026.

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

package controller

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LogActivity reads the dufs access log of a pod and returns the time of
// the last request. Health checks from the kubelet are ignored.
func LogActivity(cs kubernetes.Interface) ActivityFunc {
	return func(ctx context.Context, namespace, pod string, since time.Time) (time.Time, error) {
		opts := &corev1.PodLogOptions{Container: "dufs"}
		if !since.IsZero() {
			opts.SinceTime = &metav1.Time{Time: since}
		} else {
			opts.TailLines = new(int64(200))
		}
		rc, err := cs.CoreV1().Pods(namespace).GetLogs(pod, opts).Stream(ctx)
		if err != nil {
			return time.Time{}, err
		}
		defer func() { _ = rc.Close() }()
		return lastRequest(bufio.NewScanner(rc)), nil
	}
}

// lastRequest parses lines of '$time_iso8601 $remote_user "$request" $status'.
func lastRequest(sc *bufio.Scanner) time.Time {
	var latest time.Time
	for sc.Scan() {
		line := sc.Text()
		ts, rest, ok := strings.Cut(line, " ")
		if !ok || strings.Contains(rest, "/__dufs__/health") {
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
