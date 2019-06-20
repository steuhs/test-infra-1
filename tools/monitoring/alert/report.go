/*
Copyright 2019 The Knative Authors

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

package alert

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/knative/test-infra/tools/monitoring/config"
	"github.com/knative/test-infra/tools/monitoring/log_parser"
)

const emailTemplate = `In the past %v, 
The following error pattern reached alerting threshold:
%s

# occurrences: %v
%d jobs affected: %v
%d PRs affected: %v

Hint for diagnose & recovery: %s

Error Logs:
%v
`

// report stores list of error logs, together with sets of jobs/PRs in those logs
type report struct {
	logs []log_parser.ErrorLog
	jobs []string
	prs  []int
}

type mailContent struct {
	report
	errorPattern string
	hint         string
	window       time.Duration
}

func (c mailContent) body() string {
	return fmt.Sprintf(emailTemplate,
		c.window, c.errorPattern, len(c.logs),
		len(c.jobs), c.jobs, len(c.prs), c.prs,
		c.hint, c.sprintLogs())
}

// sprintLogs represents list of ErrorLog(s) as string
func (r report) sprintLogs() string {
	result := ""
	for i, e := range r.logs {
		result += fmt.Sprintf("%d. %s", i, e.String())
	}
	return result
}

func (c mailContent) subject() string {
	return fmt.Sprintf("Error pattern reached alerting threshold: %s", c.errorPattern)
}

// GetErrorLogs returns all jobs stored in ErrorLogs table within the time window
func GetErrorLogs(s *config.SelectedConfig, errorPattern string, db *sql.DB) ([]log_parser.ErrorLog, error) {
	var result []log_parser.ErrorLog

	// the timestamp we want to start collecting logs
	startTime := time.Now().Add(s.Duration())

	rows, err := db.Query(`
		SELECT ErrorPattern, ErrorMsg, JobName, PRNumber, BuildLogURL, TimeStamp
		FROM ErrorLogs
		WHERE ErrorPattern=? and TimeStamp > ?`,
		errorPattern, startTime)

	if err != nil {
		return result, err
	}

	for rows.Next() {
		entry := log_parser.ErrorLog{}
		err = rows.Scan(&entry.Pattern, &entry.Msg, &entry.JobName, &entry.PRNumber, &entry.BuildLogURL, &entry.TimeStamp)
		if err != nil {
			return result, err
		}
		result = append(result, entry)
	}

	return result, nil
}

func newReport(errorLogs []log_parser.ErrorLog) *report {
	report := report{logs: errorLogs}

	// Use sets to store unique values only
	jobSet := make(map[string]bool)
	prSet := make(map[int]bool)
	for _, errorLog := range errorLogs {
		if !jobSet[errorLog.JobName] {
			jobSet[errorLog.JobName] = true
			report.jobs = append(report.jobs, errorLog.JobName)
		}

		if !prSet[errorLog.PRNumber] {
			prSet[errorLog.PRNumber] = true
			report.prs = append(report.prs, errorLog.PRNumber)
		}
	}

	sort.Strings(report.jobs)
	sort.Ints(report.prs)

	return &report
}

// CheckAlertCondition checks whether the given error report meets
// the alert condition specified in config
func (r *report) CheckAlertCondition(s *config.SelectedConfig) bool {
	return len(r.logs) >= s.Occurrences &&
		len(r.jobs) >= s.JobsAffected &&
		len(r.prs) >= s.PrsAffected
}
