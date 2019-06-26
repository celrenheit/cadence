// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cassandra

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	workflow "github.com/uber/cadence/.gen/go/shared"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/log"
	p "github.com/uber/cadence/common/persistence"
	"github.com/uber/cadence/common/service/config"
)

// Guidelines for creating new special UUID constants
// Each UUID should be of the form: E0000000-R000-f000-f000-00000000000x
// Where x is any hexadecimal value, E represents the entity type valid values are:
// E = {DomainID = 1, WorkflowID = 2, RunID = 3}
// R represents row type in executions table, valid values are:
// R = {Shard = 1, Execution = 2, Transfer = 3, Timer = 4, Replication = 5}
const (
	cassandraProtoVersion = 4
	defaultSessionTimeout = 10 * time.Second
	// Special Domains related constants
	emptyDomainID = "10000000-0000-f000-f000-000000000000"
	// Special Run IDs
	emptyRunID     = "30000000-0000-f000-f000-000000000000"
	permanentRunID = "30000000-0000-f000-f000-000000000001"
	// Row Constants for Shard Row
	rowTypeShardDomainID   = "10000000-1000-f000-f000-000000000000"
	rowTypeShardWorkflowID = "20000000-1000-f000-f000-000000000000"
	rowTypeShardRunID      = "30000000-1000-f000-f000-000000000000"
	// Row Constants for Transfer Task Row
	rowTypeTransferDomainID   = "10000000-3000-f000-f000-000000000000"
	rowTypeTransferWorkflowID = "20000000-3000-f000-f000-000000000000"
	rowTypeTransferRunID      = "30000000-3000-f000-f000-000000000000"
	// Row Constants for Timer Task Row
	rowTypeTimerDomainID   = "10000000-4000-f000-f000-000000000000"
	rowTypeTimerWorkflowID = "20000000-4000-f000-f000-000000000000"
	rowTypeTimerRunID      = "30000000-4000-f000-f000-000000000000"
	// Row Constants for Replication Task Row
	rowTypeReplicationDomainID   = "10000000-5000-f000-f000-000000000000"
	rowTypeReplicationWorkflowID = "20000000-5000-f000-f000-000000000000"
	rowTypeReplicationRunID      = "30000000-5000-f000-f000-000000000000"
	// Special TaskId constants
	rowTypeExecutionTaskID  = int64(-10)
	rowTypeShardTaskID      = int64(-11)
	emptyInitiatedID        = int64(-7)
	defaultDeleteTTLSeconds = int64(time.Hour*24*7) / int64(time.Second) // keep deleted records for 7 days

	// minimum current execution retention TTL when current execution is deleted, in seconds
	minCurrentExecutionRetentionTTL = int32(24 * time.Hour / time.Second)

	stickyTaskListTTL = int32(24 * time.Hour / time.Second) // if sticky task_list stopped being updated, remove it in one day
)

const (
	// Row types for table executions
	rowTypeShard = iota
	rowTypeExecution
	rowTypeTransferTask
	rowTypeTimerTask
	rowTypeReplicationTask
)

const (
	// Row types for table tasks
	rowTypeTask = iota
	rowTypeTaskList
)

const (
	taskListTaskID = -12345
	initialRangeID = 1 // Id of the first range of a new task list
)

const (
	templateShardType = `{` +
		`shard_id: ?, ` +
		`owner: ?, ` +
		`range_id: ?, ` +
		`stolen_since_renew: ?, ` +
		`updated_at: ?, ` +
		`replication_ack_level: ?, ` +
		`transfer_ack_level: ?, ` +
		`timer_ack_level: ?, ` +
		`cluster_transfer_ack_level: ?, ` +
		`cluster_timer_ack_level: ?, ` +
		`domain_notification_version: ? ` +
		`}`

	templateWorkflowExecutionType = `{` +
		`domain_id: ?, ` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`parent_domain_id: ?, ` +
		`parent_workflow_id: ?, ` +
		`parent_run_id: ?, ` +
		`initiated_id: ?, ` +
		`completion_event_batch_id: ?, ` +
		`completion_event: ?, ` +
		`completion_event_data_encoding: ?, ` +
		`task_list: ?, ` +
		`workflow_type_name: ?, ` +
		`workflow_timeout: ?, ` +
		`decision_task_timeout: ?, ` +
		`execution_context: ?, ` +
		`state: ?, ` +
		`close_status: ?, ` +
		`last_first_event_id: ?, ` +
		`last_event_task_id: ?, ` +
		`next_event_id: ?, ` +
		`last_processed_event: ?, ` +
		`start_time: ?, ` +
		`last_updated_time: ?, ` +
		`create_request_id: ?, ` +
		`signal_count: ?, ` +
		`history_size: ?, ` +
		`decision_version: ?, ` +
		`decision_schedule_id: ?, ` +
		`decision_started_id: ?, ` +
		`decision_request_id: ?, ` +
		`decision_timeout: ?, ` +
		`decision_attempt: ?, ` +
		`decision_timestamp: ?, ` +
		`decision_scheduled_timestamp: ?, ` +
		`cancel_requested: ?, ` +
		`cancel_request_id: ?, ` +
		`sticky_task_list: ?, ` +
		`sticky_schedule_to_start_timeout: ?,` +
		`client_library_version: ?, ` +
		`client_feature_version: ?, ` +
		`client_impl: ?, ` +
		`auto_reset_points: ?, ` +
		`auto_reset_points_encoding: ?, ` +
		`attempt: ?, ` +
		`has_retry_policy: ?, ` +
		`init_interval: ?, ` +
		`backoff_coefficient: ?, ` +
		`max_interval: ?, ` +
		`expiration_time: ?, ` +
		`max_attempts: ?, ` +
		`non_retriable_errors: ?, ` +
		`event_store_version: ?, ` +
		`branch_token: ?, ` +
		`cron_schedule: ?, ` +
		`expiration_seconds: ?, ` +
		`search_attributes: ? ` +
		`}`

	templateReplicationStateType = `{` +
		`current_version: ?, ` +
		`start_version: ?, ` +
		`last_write_version: ?, ` +
		`last_write_event_id: ?, ` +
		`last_replication_info: ?` +
		`}`

	templateTransferTaskType = `{` +
		`domain_id: ?, ` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`visibility_ts: ?, ` +
		`task_id: ?, ` +
		`target_domain_id: ?, ` +
		`target_workflow_id: ?, ` +
		`target_run_id: ?, ` +
		`target_child_workflow_only: ?, ` +
		`task_list: ?, ` +
		`type: ?, ` +
		`schedule_id: ?, ` +
		`record_visibility: ?, ` +
		`version: ?` +
		`}`

	templateReplicationTaskType = `{` +
		`domain_id: ?, ` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`task_id: ?, ` +
		`type: ?, ` +
		`first_event_id: ?,` +
		`next_event_id: ?,` +
		`version: ?,` +
		`last_replication_info: ?, ` +
		`scheduled_id: ?, ` +
		`event_store_version: ?, ` +
		`branch_token: ?, ` +
		`reset_workflow: ?, ` +
		`new_run_event_store_version: ?, ` +
		`new_run_branch_token: ? ` +
		`}`

	templateTimerTaskType = `{` +
		`domain_id: ?, ` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`visibility_ts: ?, ` +
		`task_id: ?, ` +
		`type: ?, ` +
		`timeout_type: ?, ` +
		`event_id: ?, ` +
		`schedule_attempt: ?, ` +
		`version: ?` +
		`}`

	templateActivityInfoType = `{` +
		`version: ?,` +
		`schedule_id: ?, ` +
		`scheduled_event_batch_id: ?, ` +
		`scheduled_event: ?, ` +
		`scheduled_time: ?, ` +
		`started_id: ?, ` +
		`started_event: ?, ` +
		`started_time: ?, ` +
		`activity_id: ?, ` +
		`request_id: ?, ` +
		`details: ?, ` +
		`schedule_to_start_timeout: ?, ` +
		`schedule_to_close_timeout: ?, ` +
		`start_to_close_timeout: ?, ` +
		`heart_beat_timeout: ?, ` +
		`cancel_requested: ?, ` +
		`cancel_request_id: ?, ` +
		`last_hb_updated_time: ?, ` +
		`timer_task_status: ?, ` +
		`attempt: ?, ` +
		`task_list: ?, ` +
		`started_identity: ?, ` +
		`has_retry_policy: ?, ` +
		`init_interval: ?, ` +
		`backoff_coefficient: ?, ` +
		`max_interval: ?, ` +
		`expiration_time: ?, ` +
		`max_attempts: ?, ` +
		`non_retriable_errors: ?, ` +
		`event_data_encoding: ?` +
		`}`

	templateTimerInfoType = `{` +
		`version: ?,` +
		`timer_id: ?, ` +
		`started_id: ?, ` +
		`expiry_time: ?, ` +
		`task_id: ?` +
		`}`

	templateChildExecutionInfoType = `{` +
		`version: ?,` +
		`initiated_id: ?, ` +
		`initiated_event_batch_id: ?, ` +
		`initiated_event: ?, ` +
		`started_id: ?, ` +
		`started_workflow_id: ?, ` +
		`started_run_id: ?, ` +
		`started_event: ?, ` +
		`create_request_id: ?, ` +
		`event_data_encoding: ?, ` +
		`domain_name: ?, ` +
		`workflow_type_name: ?` +
		`}`

	templateRequestCancelInfoType = `{` +
		`version: ?,` +
		`initiated_id: ?, ` +
		`cancel_request_id: ? ` +
		`}`

	templateSignalInfoType = `{` +
		`version: ?,` +
		`initiated_id: ?, ` +
		`signal_request_id: ?, ` +
		`signal_name: ?, ` +
		`input: ?, ` +
		`control: ?` +
		`}`

	templateSerializedEventBatch = `{` +
		`encoding_type: ?, ` +
		`version: ?, ` +
		`data: ?` +
		`}`

	templateTaskListType = `{` +
		`domain_id: ?, ` +
		`name: ?, ` +
		`type: ?, ` +
		`ack_level: ?, ` +
		`kind: ?, ` +
		`last_updated: ? ` +
		`}`

	templateTaskType = `{` +
		`domain_id: ?, ` +
		`workflow_id: ?, ` +
		`run_id: ?, ` +
		`schedule_id: ?,` +
		`created_time: ? ` +
		`}`

	templateCreateShardQuery = `INSERT INTO executions (` +
		`shard_id, type, domain_id, workflow_id, run_id, visibility_ts, task_id, shard, range_id)` +
		`VALUES(?, ?, ?, ?, ?, ?, ?, ` + templateShardType + `, ?) IF NOT EXISTS`

	templateGetShardQuery = `SELECT shard ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ?`

	templateUpdateShardQuery = `UPDATE executions ` +
		`SET shard = ` + templateShardType + `, range_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF range_id = ?`

	templateUpdateCurrentWorkflowExecutionQuery = `UPDATE executions USING TTL 0 ` +
		`SET current_run_id = ?,
execution = {run_id: ?, create_request_id: ?, state: ?, close_status: ?},
replication_state = {start_version: ?, last_write_version: ?},
workflow_last_write_version = ?,
workflow_state = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF current_run_id = ? `

	templateUpdateCurrentWorkflowExecutionForNewQuery = templateUpdateCurrentWorkflowExecutionQuery +
		`and workflow_last_write_version = ? ` +
		`and workflow_state = ? `

	templateCreateCurrentWorkflowExecutionQuery = `INSERT INTO executions (` +
		`shard_id, type, domain_id, workflow_id, run_id, visibility_ts, task_id, current_run_id, execution, replication_state, workflow_last_write_version, workflow_state) ` +
		`VALUES(?, ?, ?, ?, ?, ?, ?, ?, {run_id: ?, create_request_id: ?, state: ?, close_status: ?}, {start_version: ?, last_write_version: ?}, ?, ?) IF NOT EXISTS USING TTL 0 `

	templateCreateWorkflowExecutionQuery = `INSERT INTO executions (` +
		`shard_id, domain_id, workflow_id, run_id, type, execution, next_event_id, visibility_ts, task_id) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateWorkflowExecutionType + `, ?, ?, ?) `

	templateCreateWorkflowExecutionWithReplicationQuery = `INSERT INTO executions (` +
		`shard_id, domain_id, workflow_id, run_id, type, execution, replication_state, next_event_id, visibility_ts, task_id) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateWorkflowExecutionType + `, ` + templateReplicationStateType + `, ?, ?, ?) `

	templateCreateWorkflowExecutionWithVersionHistoriesQuery = `INSERT INTO executions (` +
		`shard_id, domain_id, workflow_id, run_id, type, execution, next_event_id, visibility_ts, task_id, version_histories, version_histories_encoding) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateWorkflowExecutionType + `, ?, ?, ?, ?, ?) `

	templateCreateTransferTaskQuery = `INSERT INTO executions (` +
		`shard_id, type, domain_id, workflow_id, run_id, transfer, visibility_ts, task_id) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateTransferTaskType + `, ?, ?)`

	templateCreateReplicationTaskQuery = `INSERT INTO executions (` +
		`shard_id, type, domain_id, workflow_id, run_id, replication, visibility_ts, task_id) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateReplicationTaskType + `, ?, ?)`

	templateCreateTimerTaskQuery = `INSERT INTO executions (` +
		`shard_id, type, domain_id, workflow_id, run_id, timer, visibility_ts, task_id) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateTimerTaskType + `, ?, ?)`

	templateUpdateLeaseQuery = `UPDATE executions ` +
		`SET range_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF range_id = ?`

	templateGetWorkflowExecutionQuery = `SELECT execution, replication_state, activity_map, timer_map, child_executions_map, request_cancel_map, signal_map, signal_requested, buffered_events_list, buffered_replication_tasks_map, version_histories, version_histories_encoding ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ?`

	templateGetCurrentExecutionQuery = `SELECT current_run_id, execution, replication_state ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ?`

	templateCheckWorkflowExecutionQuery = `UPDATE executions ` +
		`SET next_event_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateUpdateWorkflowExecutionConditionSuffix = ` IF next_event_id = ?`

	templateUpdateWorkflowExecutionQuery = `UPDATE executions ` +
		`SET execution = ` + templateWorkflowExecutionType +
		`, next_event_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateWorkflowExecutionWithReplicationQuery = `UPDATE executions ` +
		`SET execution = ` + templateWorkflowExecutionType +
		`, replication_state = ` + templateReplicationStateType +
		`, next_event_id = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateWorkflowExecutionWithVersionHistoriesQuery = `UPDATE executions ` +
		`SET execution = ` + templateWorkflowExecutionType +
		`, next_event_id = ? ` +
		`, version_histories = ? ` +
		`, version_histories_encoding = ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateActivityInfoQuery = `UPDATE executions ` +
		`SET activity_map[ ? ] =` + templateActivityInfoType + ` ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetActivityInfoQuery = `UPDATE executions ` +
		`SET activity_map = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateTimerInfoQuery = `UPDATE executions ` +
		`SET timer_map[ ? ] =` + templateTimerInfoType + ` ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetTimerInfoQuery = `UPDATE executions ` +
		`SET timer_map = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateChildExecutionInfoQuery = `UPDATE executions ` +
		`SET child_executions_map[ ? ] =` + templateChildExecutionInfoType + ` ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetChildExecutionInfoQuery = `UPDATE executions ` +
		`SET child_executions_map = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateRequestCancelInfoQuery = `UPDATE executions ` +
		`SET request_cancel_map[ ? ] =` + templateRequestCancelInfoType + ` ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetRequestCancelInfoQuery = `UPDATE executions ` +
		`SET request_cancel_map = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateSignalInfoQuery = `UPDATE executions ` +
		`SET signal_map[ ? ] =` + templateSignalInfoType + ` ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetSignalInfoQuery = `UPDATE executions ` +
		`SET signal_map = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateUpdateSignalRequestedQuery = `UPDATE executions ` +
		`SET signal_requested = signal_requested + ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateResetSignalRequestedQuery = `UPDATE executions ` +
		`SET signal_requested = ?` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateAppendBufferedEventsQuery = `UPDATE executions ` +
		`SET buffered_events_list = buffered_events_list + ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteBufferedEventsQuery = `UPDATE executions ` +
		`SET buffered_events_list = [] ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteActivityInfoQuery = `DELETE activity_map[ ? ] ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteTimerInfoQuery = `DELETE timer_map[ ? ] ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteChildExecutionInfoQuery = `DELETE child_executions_map[ ? ] ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteRequestCancelInfoQuery = `DELETE request_cancel_map[ ? ] ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteSignalInfoQuery = `DELETE signal_map[ ? ] ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateDeleteWorkflowExecutionMutableStateQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? `

	templateDeleteWorkflowExecutionCurrentRowQuery = templateDeleteWorkflowExecutionMutableStateQuery + " if current_run_id = ? "

	templateDeleteWorkflowExecutionSignalRequestedQuery = `UPDATE executions ` +
		`SET signal_requested = signal_requested - ? ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ? ` +
		`IF next_event_id = ?`

	templateGetTransferTasksQuery = `SELECT transfer ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id > ? ` +
		`and task_id <= ?`

	templateGetReplicationTasksQuery = `SELECT replication ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id > ? ` +
		`and task_id <= ?`

	templateCompleteTransferTaskQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id = ?`

	templateRangeCompleteTransferTaskQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ? ` +
		`and run_id = ? ` +
		`and visibility_ts = ? ` +
		`and task_id > ? ` +
		`and task_id <= ?`

	templateGetTimerTasksQuery = `SELECT timer ` +
		`FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ?` +
		`and domain_id = ? ` +
		`and workflow_id = ?` +
		`and run_id = ?` +
		`and visibility_ts >= ? ` +
		`and visibility_ts < ?`

	templateCompleteTimerTaskQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ?` +
		`and run_id = ?` +
		`and visibility_ts = ? ` +
		`and task_id = ?`

	templateRangeCompleteTimerTaskQuery = `DELETE FROM executions ` +
		`WHERE shard_id = ? ` +
		`and type = ? ` +
		`and domain_id = ? ` +
		`and workflow_id = ?` +
		`and run_id = ?` +
		`and visibility_ts >= ? ` +
		`and visibility_ts < ?`

	templateCreateTaskQuery = `INSERT INTO tasks (` +
		`domain_id, task_list_name, task_list_type, type, task_id, task) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateTaskType + `)`

	templateCreateTaskWithTTLQuery = `INSERT INTO tasks (` +
		`domain_id, task_list_name, task_list_type, type, task_id, task) ` +
		`VALUES(?, ?, ?, ?, ?, ` + templateTaskType + `) USING TTL ?`

	templateGetTasksQuery = `SELECT task_id, task ` +
		`FROM tasks ` +
		`WHERE domain_id = ? ` +
		`and task_list_name = ? ` +
		`and task_list_type = ? ` +
		`and type = ? ` +
		`and task_id > ? ` +
		`and task_id <= ?`

	templateCompleteTaskQuery = `DELETE FROM tasks ` +
		`WHERE domain_id = ? ` +
		`and task_list_name = ? ` +
		`and task_list_type = ? ` +
		`and type = ? ` +
		`and task_id = ?`

	templateCompleteTasksLessThanQuery = `DELETE FROM tasks ` +
		`WHERE domain_id = ? ` +
		`AND task_list_name = ? ` +
		`AND task_list_type = ? ` +
		`AND type = ? ` +
		`AND task_id <= ? `

	templateGetTaskList = `SELECT ` +
		`range_id, ` +
		`task_list ` +
		`FROM tasks ` +
		`WHERE domain_id = ? ` +
		`and task_list_name = ? ` +
		`and task_list_type = ? ` +
		`and type = ? ` +
		`and task_id = ?`

	templateInsertTaskListQuery = `INSERT INTO tasks (` +
		`domain_id, ` +
		`task_list_name, ` +
		`task_list_type, ` +
		`type, ` +
		`task_id, ` +
		`range_id, ` +
		`task_list ` +
		`) VALUES (?, ?, ?, ?, ?, ?, ` + templateTaskListType + `) IF NOT EXISTS`

	templateUpdateTaskListQuery = `UPDATE tasks SET ` +
		`range_id = ?, ` +
		`task_list = ` + templateTaskListType + " " +
		`WHERE domain_id = ? ` +
		`and task_list_name = ? ` +
		`and task_list_type = ? ` +
		`and type = ? ` +
		`and task_id = ? ` +
		`IF range_id = ?`

	templateUpdateTaskListQueryWithTTL = `INSERT INTO tasks (` +
		`domain_id, ` +
		`task_list_name, ` +
		`task_list_type, ` +
		`type, ` +
		`task_id, ` +
		`range_id, ` +
		`task_list ` +
		`) VALUES (?, ?, ?, ?, ?, ?, ` + templateTaskListType + `) USING TTL ?`

	templateDeleteTaskListQuery = `DELETE FROM tasks ` +
		`WHERE domain_id = ? ` +
		`AND task_list_name = ? ` +
		`AND task_list_type = ? ` +
		`AND type = ? ` +
		`AND task_id = ? ` +
		`IF range_id = ?`
)

var (
	defaultDateTime            = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	defaultVisibilityTimestamp = p.UnixNanoToDBTimestamp(defaultDateTime.UnixNano())
)

type (
	cassandraStore struct {
		session *gocql.Session
		logger  log.Logger
	}

	// Implements ExecutionManager, ShardManager and TaskManager
	cassandraPersistence struct {
		cassandraStore
		shardID            int
		currentClusterName string
	}
)

var _ p.ExecutionStore = (*cassandraPersistence)(nil)

// newShardPersistence is used to create an instance of ShardManager implementation
func newShardPersistence(cfg config.Cassandra, clusterName string, logger log.Logger) (p.ShardStore, error) {
	cluster := NewCassandraCluster(cfg.Hosts, cfg.Port, cfg.User, cfg.Password, cfg.Datacenter)
	cluster.Keyspace = cfg.Keyspace
	cluster.ProtoVersion = cassandraProtoVersion
	cluster.Consistency = gocql.LocalQuorum
	cluster.SerialConsistency = gocql.LocalSerial
	cluster.Timeout = defaultSessionTimeout

	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}

	return &cassandraPersistence{
		cassandraStore:     cassandraStore{session: session, logger: logger},
		shardID:            -1,
		currentClusterName: clusterName,
	}, nil
}

// NewWorkflowExecutionPersistence is used to create an instance of workflowExecutionManager implementation
func NewWorkflowExecutionPersistence(shardID int, session *gocql.Session,
	logger log.Logger) (p.ExecutionStore, error) {
	return &cassandraPersistence{cassandraStore: cassandraStore{session: session, logger: logger}, shardID: shardID}, nil
}

// newTaskPersistence is used to create an instance of TaskManager implementation
func newTaskPersistence(cfg config.Cassandra, logger log.Logger) (p.TaskStore, error) {
	cluster := NewCassandraCluster(cfg.Hosts, cfg.Port, cfg.User, cfg.Password, cfg.Datacenter)
	cluster.Keyspace = cfg.Keyspace
	cluster.ProtoVersion = cassandraProtoVersion
	cluster.Consistency = gocql.LocalQuorum
	cluster.SerialConsistency = gocql.LocalSerial
	cluster.Timeout = defaultSessionTimeout
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}
	return &cassandraPersistence{cassandraStore: cassandraStore{session: session, logger: logger}, shardID: -1}, nil
}

func (d *cassandraStore) GetName() string {
	return cassandraPersistenceName
}

// Close releases the underlying resources held by this object
func (d *cassandraStore) Close() {
	if d.session != nil {
		d.session.Close()
	}
}

func (d *cassandraPersistence) GetShardID() int {
	return d.shardID
}

func (d *cassandraPersistence) CreateShard(request *p.CreateShardRequest) error {
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())
	shardInfo := request.ShardInfo
	query := d.session.Query(templateCreateShardQuery,
		shardInfo.ShardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		shardInfo.ShardID,
		shardInfo.Owner,
		shardInfo.RangeID,
		shardInfo.StolenSinceRenew,
		cqlNowTimestamp,
		shardInfo.ReplicationAckLevel,
		shardInfo.TransferAckLevel,
		shardInfo.TimerAckLevel,
		shardInfo.ClusterTransferAckLevel,
		shardInfo.ClusterTimerAckLevel,
		shardInfo.DomainNotificationVersion,
		shardInfo.RangeID)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CreateShard operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateShard operation failed. Error: %v", err),
		}
	}

	if !applied {
		shard := previous["shard"].(map[string]interface{})
		return &p.ShardAlreadyExistError{
			Msg: fmt.Sprintf("Shard already exists in executions table.  ShardId: %v, RangeId: %v",
				shard["shard_id"], shard["range_id"]),
		}
	}

	return nil
}

func (d *cassandraPersistence) GetShard(request *p.GetShardRequest) (*p.GetShardResponse, error) {
	shardID := request.ShardID
	query := d.session.Query(templateGetShardQuery,
		shardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID)

	result := make(map[string]interface{})
	if err := query.MapScan(result); err != nil {
		if err == gocql.ErrNotFound {
			return nil, &workflow.EntityNotExistsError{
				Message: fmt.Sprintf("Shard not found.  ShardId: %v", shardID),
			}
		} else if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("GetShard operation failed. Error: %v", err),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetShard operation failed. Error: %v", err),
		}
	}

	info := createShardInfo(d.currentClusterName, result["shard"].(map[string]interface{}))

	return &p.GetShardResponse{ShardInfo: info}, nil
}

func (d *cassandraPersistence) UpdateShard(request *p.UpdateShardRequest) error {
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())
	shardInfo := request.ShardInfo

	query := d.session.Query(templateUpdateShardQuery,
		shardInfo.ShardID,
		shardInfo.Owner,
		shardInfo.RangeID,
		shardInfo.StolenSinceRenew,
		cqlNowTimestamp,
		shardInfo.ReplicationAckLevel,
		shardInfo.TransferAckLevel,
		shardInfo.TimerAckLevel,
		shardInfo.ClusterTransferAckLevel,
		shardInfo.ClusterTimerAckLevel,
		shardInfo.DomainNotificationVersion,
		shardInfo.RangeID,
		shardInfo.ShardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		request.PreviousRangeID)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("UpdateShard operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("UpdateShard operation failed. Error: %v", err),
		}
	}

	if !applied {
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}

		return &p.ShardOwnershipLostError{
			ShardID: d.shardID,
			Msg: fmt.Sprintf("Failed to update shard.  previous_range_id: %v, columns: (%v)",
				request.PreviousRangeID, strings.Join(columns, ",")),
		}
	}

	return nil
}

func (d *cassandraPersistence) CreateWorkflowExecution(request *p.InternalCreateWorkflowExecutionRequest) (
	*p.CreateWorkflowExecutionResponse, error) {
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())
	batch := d.session.NewBatch(gocql.LoggedBatch)

	if request.CreateWorkflowMode == p.CreateWorkflowModeZombie {
		if err := d.assertNotCurrentExecution(
			request.DomainID,
			request.Execution.GetWorkflowId(),
			request.Execution.GetRunId()); err != nil {
			return nil, err
		}
	}
	if err := d.CreateWorkflowExecutionWithinBatch(request, batch, cqlNowTimestamp); err != nil {
		return nil, err
	}

	d.createTransferTasks(batch, request.TransferTasks, request.DomainID, *request.Execution.WorkflowId,
		*request.Execution.RunId)
	d.createReplicationTasks(batch, request.ReplicationTasks, request.DomainID, *request.Execution.WorkflowId,
		*request.Execution.RunId)
	d.createTimerTasks(batch, request.TimerTasks, request.DomainID, *request.Execution.WorkflowId,
		*request.Execution.RunId, cqlNowTimestamp)

	batch.Query(templateUpdateLeaseQuery,
		request.RangeID,
		d.shardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		request.RangeID,
	)

	previous := make(map[string]interface{})
	applied, iter, err := d.session.MapExecuteBatchCAS(batch, previous)
	defer func() {
		if iter != nil {
			iter.Close()
		}
	}()

	if err != nil {
		if isTimeoutError(err) {
			// Write may have succeeded, but we don't know
			// return this info to the caller so they have the option of trying to find out by executing a read
			return nil, &p.TimeoutError{Msg: fmt.Sprintf("CreateWorkflowExecution timed out. Error: %v", err)}
		} else if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CreateWorkflowExecution operation failed. Error: %v", err),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateWorkflowExecution operation failed. Error: %v", err),
		}
	}

	if !applied {
		// There can be two reasons why the query does not get applied. Either the RangeID has changed, or
		// the workflow is already started. Check the row info returned by Cassandra to figure out which one it is.
	GetFailureReasonLoop:
		for {
			rowType, ok := previous["type"].(int)
			if !ok {
				// This should never happen, as all our rows have the type field.
				break GetFailureReasonLoop
			}
			runID := previous["run_id"].(gocql.UUID).String()

			if rowType == rowTypeShard {
				if rangeID, ok := previous["range_id"].(int64); ok && rangeID != request.RangeID {
					// CreateWorkflowExecution failed because rangeID was modified
					return nil, &p.ShardOwnershipLostError{
						ShardID: d.shardID,
						Msg: fmt.Sprintf("Failed to create workflow execution.  Request RangeID: %v, Actual RangeID: %v",
							request.RangeID, rangeID),
					}
				}

			} else if rowType == rowTypeExecution && runID == permanentRunID {
				var columns []string
				for k, v := range previous {
					columns = append(columns, fmt.Sprintf("%s=%v", k, v))
				}

				if execution, ok := previous["execution"].(map[string]interface{}); ok {
					// CreateWorkflowExecution failed because it already exists
					executionInfo := createWorkflowExecutionInfo(execution)

					lastWriteVersion := common.EmptyVersion
					if request.ReplicationState != nil {
						replicationState := createReplicationState(previous["replication_state"].(map[string]interface{}))
						lastWriteVersion = replicationState.LastWriteVersion
					}

					msg := fmt.Sprintf("Workflow execution already running. WorkflowId: %v, RunId: %v, rangeID: %v, columns: (%v)",
						request.Execution.GetWorkflowId(), executionInfo.RunID, request.RangeID, strings.Join(columns, ","))
					return nil, &p.WorkflowExecutionAlreadyStartedError{
						Msg:              msg,
						StartRequestID:   executionInfo.CreateRequestID,
						RunID:            executionInfo.RunID,
						State:            executionInfo.State,
						CloseStatus:      executionInfo.CloseStatus,
						LastWriteVersion: lastWriteVersion,
					}
				}

				if prevRunID := previous["current_run_id"].(gocql.UUID).String(); prevRunID != request.Execution.GetRunId() {
					// currentRunID on previous run has been changed, return to caller to handle
					msg := fmt.Sprintf("Workflow execution creation condition failed by mismatch runID. WorkflowId: %v, CurrentRunID: %v, columns: (%v)",
						request.Execution.GetWorkflowId(), request.Execution.GetRunId(), strings.Join(columns, ","))
					return nil, &p.CurrentWorkflowConditionFailedError{Msg: msg}
				}

				msg := fmt.Sprintf("Workflow execution creation condition failed. WorkflowId: %v, CurrentRunID: %v, columns: (%v)",
					request.Execution.GetWorkflowId(), request.Execution.GetRunId(), strings.Join(columns, ","))
				return nil, &p.ConditionFailedError{Msg: msg}
			}

			previous = make(map[string]interface{})
			if !iter.MapScan(previous) {
				// Cassandra returns the actual row that caused a condition failure, so we should always return
				// from the checks above, but just in case.
				break GetFailureReasonLoop
			}
		}

		// At this point we only know that the write was not applied.
		// It's much safer to return ShardOwnershipLostError as the default to force the application to reload
		// shard to recover from such errors
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}
		return nil, &p.ShardOwnershipLostError{
			ShardID: d.shardID,
			Msg: fmt.Sprintf("Failed to create workflow execution.  Request RangeID: %v, columns: (%v)",
				request.RangeID, strings.Join(columns, ",")),
		}
	}

	return &p.CreateWorkflowExecutionResponse{}, nil
}

func (d *cassandraPersistence) CreateWorkflowExecutionWithinBatch(request *p.InternalCreateWorkflowExecutionRequest,
	batch *gocql.Batch, cqlNowTimestamp int64) error {
	// validate workflow state & close status
	if err := p.ValidateCreateWorkflowStateCloseStatus(
		request.State,
		request.CloseStatus); err != nil {
		return err
	}

	parentDomainID := emptyDomainID
	parentWorkflowID := ""
	parentRunID := emptyRunID
	initiatedID := emptyInitiatedID
	if request.ParentDomainID != "" {
		parentDomainID = request.ParentDomainID
		parentWorkflowID = request.ParentExecution.GetWorkflowId()
		parentRunID = request.ParentExecution.GetRunId()
		initiatedID = request.InitiatedID
	}

	startVersion := common.EmptyVersion
	lastWriteVersion := common.EmptyVersion
	if request.ReplicationState != nil {
		startVersion = request.ReplicationState.StartVersion
		lastWriteVersion = request.ReplicationState.LastWriteVersion
	} else {
		// this is to deal with issue that gocql cannot return null value for value inside user defined type
		// so we cannot know whether the last write version of current workflow record is null or not
		// since non global domain (request.ReplicationState == null) will not have workflow reset problem
		// so the CAS on last write version is not necessary
		if request.CreateWorkflowMode == p.CreateWorkflowModeWorkflowIDReuse {
			request.CreateWorkflowMode = p.CreateWorkflowModeContinueAsNew
		}
	}
	switch request.CreateWorkflowMode {
	case p.CreateWorkflowModeContinueAsNew:
		batch.Query(templateUpdateCurrentWorkflowExecutionQuery,
			request.Execution.GetRunId(),
			request.Execution.GetRunId(),
			request.RequestID,
			request.State,
			request.CloseStatus,
			startVersion,
			lastWriteVersion,
			lastWriteVersion,
			request.State,
			d.shardID,
			rowTypeExecution,
			request.DomainID,
			*request.Execution.WorkflowId,
			permanentRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			request.PreviousRunID,
		)
	case p.CreateWorkflowModeWorkflowIDReuse:
		batch.Query(templateUpdateCurrentWorkflowExecutionForNewQuery,
			*request.Execution.RunId,
			*request.Execution.RunId,
			request.RequestID,
			request.State,
			request.CloseStatus,
			startVersion,
			lastWriteVersion,
			lastWriteVersion,
			request.State,
			d.shardID,
			rowTypeExecution,
			request.DomainID,
			*request.Execution.WorkflowId,
			permanentRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			request.PreviousRunID,
			request.PreviousLastWriteVersion,
			p.WorkflowStateCompleted,
		)
	case p.CreateWorkflowModeBrandNew:
		batch.Query(templateCreateCurrentWorkflowExecutionQuery,
			d.shardID,
			rowTypeExecution,
			request.DomainID,
			*request.Execution.WorkflowId,
			permanentRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			*request.Execution.RunId,
			*request.Execution.RunId,
			request.RequestID,
			request.State,
			request.CloseStatus,
			startVersion,
			lastWriteVersion,
			lastWriteVersion,
			request.State,
		)
	case p.CreateWorkflowModeZombie:
		// for creation of zombie workflow, check that
		// 1. workflow state being WorkflowStateZombie
		// 2. workflow close status being WorkflowCloseStatusNone
		if request.State != p.WorkflowStateZombie || request.CloseStatus != p.WorkflowCloseStatusNone {
			return &workflow.InternalServiceError{Message: fmt.Sprintf(
				"Cannot create zombie workflow with state: %v, close status: %v",
				request.State,
				request.CloseStatus,
			)}
		}
	default:
		panic(fmt.Sprintf("Unknown CreateWorkflowMode: %v", request.CreateWorkflowMode))
	}

	// TODO use updateMutableState() with useCondition=false to make code much cleaner
	if request.ReplicationState != nil {
		lastReplicationInfo := make(map[string]map[string]interface{})
		for k, v := range request.ReplicationState.LastReplicationInfo {
			lastReplicationInfo[k] = createReplicationInfoMap(v)
		}

		batch.Query(templateCreateWorkflowExecutionWithReplicationQuery,
			d.shardID,
			request.DomainID,
			request.Execution.GetWorkflowId(),
			request.Execution.GetRunId(),
			rowTypeExecution,
			request.DomainID,
			request.Execution.GetWorkflowId(),
			request.Execution.GetRunId(),
			parentDomainID,
			parentWorkflowID,
			parentRunID,
			initiatedID,
			common.EmptyEventID,
			nil,
			"",
			request.TaskList,
			request.WorkflowTypeName,
			request.WorkflowTimeout,
			request.DecisionTimeoutValue,
			request.ExecutionContext,
			request.State,
			request.CloseStatus,
			common.FirstEventID,
			request.LastEventTaskID,
			request.NextEventID,
			request.LastProcessedEvent,
			cqlNowTimestamp,
			cqlNowTimestamp,
			request.RequestID,
			request.SignalCount,
			request.HistorySize,
			request.DecisionVersion,
			request.DecisionScheduleID,
			request.DecisionStartedID,
			"", // Decision Start Request ID
			request.DecisionStartToCloseTimeout,
			0,     // decision attempts
			0,     // decision timestamp(started time)
			0,     // decision scheduled timestamp
			false, // cancel_requested
			"",    // cancel_request_id
			"",    // sticky_task_list (no sticky tasklist for new workflow execution)
			0,     // sticky_schedule_to_start_timeout
			"",    // client_library_version
			"",    // client_feature_version
			"",    // client_impl
			request.PreviousAutoResetPoints.Data,
			request.PreviousAutoResetPoints.GetEncoding(),
			request.Attempt,
			request.HasRetryPolicy,
			request.InitialInterval,
			request.BackoffCoefficient,
			request.MaximumInterval,
			request.ExpirationTime,
			request.MaximumAttempts,
			request.NonRetriableErrors,
			request.EventStoreVersion,
			request.BranchToken,
			request.CronSchedule,
			request.ExpirationSeconds,
			request.SearchAttributes,
			request.ReplicationState.CurrentVersion,
			request.ReplicationState.StartVersion,
			request.ReplicationState.LastWriteVersion,
			request.ReplicationState.LastWriteEventID,
			lastReplicationInfo,
			request.NextEventID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID)
	} else if request.VersionHistories != nil {
		versionHistoriesData, versionHistoriesEncoding := p.FromDataBlob(request.VersionHistories)
		batch.Query(templateCreateWorkflowExecutionWithVersionHistoriesQuery,
			d.shardID,
			request.DomainID,
			*request.Execution.WorkflowId,
			*request.Execution.RunId,
			rowTypeExecution,
			request.DomainID,
			*request.Execution.WorkflowId,
			*request.Execution.RunId,
			parentDomainID,
			parentWorkflowID,
			parentRunID,
			initiatedID,
			common.EmptyEventID,
			nil,
			"",
			request.TaskList,
			request.WorkflowTypeName,
			request.WorkflowTimeout,
			request.DecisionTimeoutValue,
			request.ExecutionContext,
			request.State,
			request.CloseStatus,
			common.FirstEventID,
			request.LastEventTaskID,
			request.NextEventID,
			request.LastProcessedEvent,
			cqlNowTimestamp,
			cqlNowTimestamp,
			request.RequestID,
			request.SignalCount,
			request.HistorySize,
			request.DecisionVersion,
			request.DecisionScheduleID,
			request.DecisionStartedID,
			"", // Decision Start Request ID
			request.DecisionStartToCloseTimeout,
			0,     // decision attempts
			0,     // decision timestamp(started time)
			0,     // decision scheduled timestamp
			false, // cancel_requested
			"",    // cancel_request_id
			"",    // sticky_task_list (no sticky tasklist for new workflow execution)
			0,     // sticky_schedule_to_start_timeout
			"",    // client_library_version
			"",    // client_feature_version
			"",    // client_impl
			request.PreviousAutoResetPoints.Data,
			request.PreviousAutoResetPoints.GetEncoding(),
			request.Attempt,
			request.HasRetryPolicy,
			request.InitialInterval,
			request.BackoffCoefficient,
			request.MaximumInterval,
			request.ExpirationTime,
			request.MaximumAttempts,
			request.NonRetriableErrors,
			request.EventStoreVersion,
			request.BranchToken,
			request.CronSchedule,
			request.ExpirationSeconds,
			request.SearchAttributes,
			request.NextEventID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			versionHistoriesData,
			versionHistoriesEncoding)
	} else {
		// Cross DC feature is currently disabled so we will be creating workflow executions without replication state
		batch.Query(templateCreateWorkflowExecutionQuery,
			d.shardID,
			request.DomainID,
			request.Execution.GetWorkflowId(),
			request.Execution.GetRunId(),
			rowTypeExecution,
			request.DomainID,
			request.Execution.GetWorkflowId(),
			request.Execution.GetRunId(),
			parentDomainID,
			parentWorkflowID,
			parentRunID,
			initiatedID,
			common.EmptyEventID,
			nil,
			"",
			request.TaskList,
			request.WorkflowTypeName,
			request.WorkflowTimeout,
			request.DecisionTimeoutValue,
			request.ExecutionContext,
			request.State,
			request.CloseStatus,
			common.FirstEventID,
			request.LastEventTaskID,
			request.NextEventID,
			request.LastProcessedEvent,
			cqlNowTimestamp,
			cqlNowTimestamp,
			request.RequestID,
			request.SignalCount,
			request.HistorySize,
			request.DecisionVersion,
			request.DecisionScheduleID,
			request.DecisionStartedID,
			"", // Decision Start Request ID
			request.DecisionStartToCloseTimeout,
			0,     // decision attempts
			0,     // decision timestamp(started time)
			0,     // decision scheduled timestamp
			false, // cancel_requested
			"",    // cancel_request_id
			"",    // sticky_task_list (no sticky tasklist for new workflow execution)
			0,     // sticky_schedule_to_start_timeout
			"",    // client_library_version
			"",    // client_feature_version
			"",    // client_impl
			request.PreviousAutoResetPoints.Data,
			request.PreviousAutoResetPoints.GetEncoding(),
			request.Attempt,
			request.HasRetryPolicy,
			request.InitialInterval,
			request.BackoffCoefficient,
			request.MaximumInterval,
			request.ExpirationTime,
			request.MaximumAttempts,
			request.NonRetriableErrors,
			request.EventStoreVersion,
			request.BranchToken,
			request.CronSchedule,
			request.ExpirationSeconds,
			request.SearchAttributes,
			request.NextEventID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID)
	}
	return nil
}

func (d *cassandraPersistence) GetWorkflowExecution(request *p.GetWorkflowExecutionRequest) (
	*p.InternalGetWorkflowExecutionResponse, error) {
	execution := request.Execution
	query := d.session.Query(templateGetWorkflowExecutionQuery,
		d.shardID,
		rowTypeExecution,
		request.DomainID,
		*execution.WorkflowId,
		*execution.RunId,
		defaultVisibilityTimestamp,
		rowTypeExecutionTaskID)

	result := make(map[string]interface{})
	if err := query.MapScan(result); err != nil {
		if err == gocql.ErrNotFound {
			return nil, &workflow.EntityNotExistsError{
				Message: fmt.Sprintf("Workflow execution not found.  WorkflowId: %v, RunId: %v",
					*execution.WorkflowId, *execution.RunId),
			}
		} else if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("GetWorkflowExecution operation failed. Error: %v", err),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetWorkflowExecution operation failed. Error: %v", err),
		}
	}

	state := &p.InternalWorkflowMutableState{}
	info := createWorkflowExecutionInfo(result["execution"].(map[string]interface{}))
	state.ExecutionInfo = info

	replicationState := createReplicationState(result["replication_state"].(map[string]interface{}))
	state.ReplicationState = replicationState

	activityInfos := make(map[int64]*p.InternalActivityInfo)
	aMap := result["activity_map"].(map[int64]map[string]interface{})
	for key, value := range aMap {
		info := createActivityInfo(request.DomainID, value)
		activityInfos[key] = info
	}
	state.ActivityInfos = activityInfos

	timerInfos := make(map[string]*p.TimerInfo)
	tMap := result["timer_map"].(map[string]map[string]interface{})
	for key, value := range tMap {
		info := createTimerInfo(value)
		timerInfos[key] = info
	}
	state.TimerInfos = timerInfos

	childExecutionInfos := make(map[int64]*p.InternalChildExecutionInfo)
	cMap := result["child_executions_map"].(map[int64]map[string]interface{})
	for key, value := range cMap {
		info := createChildExecutionInfo(value, d.logger)
		childExecutionInfos[key] = info
	}
	state.ChildExecutionInfos = childExecutionInfos

	requestCancelInfos := make(map[int64]*p.RequestCancelInfo)
	rMap := result["request_cancel_map"].(map[int64]map[string]interface{})
	for key, value := range rMap {
		info := createRequestCancelInfo(value)
		requestCancelInfos[key] = info
	}
	state.RequestCancelInfos = requestCancelInfos

	signalInfos := make(map[int64]*p.SignalInfo)
	sMap := result["signal_map"].(map[int64]map[string]interface{})
	for key, value := range sMap {
		info := createSignalInfo(value)
		signalInfos[key] = info
	}
	state.SignalInfos = signalInfos

	signalRequestedIDs := make(map[string]struct{})
	sList := result["signal_requested"].([]gocql.UUID)
	for _, v := range sList {
		signalRequestedIDs[v.String()] = struct{}{}
	}
	state.SignalRequestedIDs = signalRequestedIDs

	eList := result["buffered_events_list"].([]map[string]interface{})
	bufferedEventsBlobs := make([]*p.DataBlob, 0, len(eList))
	for _, v := range eList {
		blob := createHistoryEventBatchBlob(v)
		bufferedEventsBlobs = append(bufferedEventsBlobs, blob)
	}
	state.BufferedEvents = bufferedEventsBlobs

	state.VersionHistories = p.NewDataBlob(result["version_histories"].([]byte), common.EncodingType(result["version_histories_encoding"].(string)))
	if state.VersionHistories != nil && state.ReplicationState != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetWorkflowExecution operation failed. VersionHistories and ReplicationState both are set."),
		}
	}

	return &p.InternalGetWorkflowExecutionResponse{State: state}, nil
}

// this helper is passing dynamic number of arguments based on whether needing condition or not
func batchQueryHelper(batch *gocql.Batch, stmt string, useCondition bool, condition int64, args ...interface{}) {
	if useCondition {
		stmt += templateUpdateWorkflowExecutionConditionSuffix
		args = append(args, condition)
	}
	batch.Query(stmt, args...)
}

func (d *cassandraPersistence) updateMutableState(batch *gocql.Batch, executionInfo *p.InternalWorkflowExecutionInfo,
	replicationState *p.ReplicationState, versionHistories *p.DataBlob, cqlNowTimestamp int64, useCondition bool, condition int64) {
	if executionInfo.ParentDomainID == "" {
		executionInfo.ParentDomainID = emptyDomainID
	}
	if executionInfo.ParentRunID == "" {
		executionInfo.ParentRunID = emptyRunID
	}

	completionData, completionEncoding := p.FromDataBlob(executionInfo.CompletionEvent)
	if replicationState != nil {
		lastReplicationInfo := make(map[string]map[string]interface{})
		for k, v := range replicationState.LastReplicationInfo {
			lastReplicationInfo[k] = createReplicationInfoMap(v)
		}

		batchQueryHelper(batch, templateUpdateWorkflowExecutionWithReplicationQuery, useCondition, condition,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			executionInfo.ParentDomainID,
			executionInfo.ParentWorkflowID,
			executionInfo.ParentRunID,
			executionInfo.InitiatedID,
			executionInfo.CompletionEventBatchID,
			completionData,
			completionEncoding,
			executionInfo.TaskList,
			executionInfo.WorkflowTypeName,
			executionInfo.WorkflowTimeout,
			executionInfo.DecisionTimeoutValue,
			executionInfo.ExecutionContext,
			executionInfo.State,
			executionInfo.CloseStatus,
			executionInfo.LastFirstEventID,
			executionInfo.LastEventTaskID,
			executionInfo.NextEventID,
			executionInfo.LastProcessedEvent,
			executionInfo.StartTimestamp,
			cqlNowTimestamp,
			executionInfo.CreateRequestID,
			executionInfo.SignalCount,
			executionInfo.HistorySize,
			executionInfo.DecisionVersion,
			executionInfo.DecisionScheduleID,
			executionInfo.DecisionStartedID,
			executionInfo.DecisionRequestID,
			executionInfo.DecisionTimeout,
			executionInfo.DecisionAttempt,
			executionInfo.DecisionStartedTimestamp,
			executionInfo.DecisionScheduledTimestamp,
			executionInfo.CancelRequested,
			executionInfo.CancelRequestID,
			executionInfo.StickyTaskList,
			executionInfo.StickyScheduleToStartTimeout,
			executionInfo.ClientLibraryVersion,
			executionInfo.ClientFeatureVersion,
			executionInfo.ClientImpl,
			executionInfo.AutoResetPoints.Data,
			executionInfo.AutoResetPoints.GetEncoding(),
			executionInfo.Attempt,
			executionInfo.HasRetryPolicy,
			executionInfo.InitialInterval,
			executionInfo.BackoffCoefficient,
			executionInfo.MaximumInterval,
			executionInfo.ExpirationTime,
			executionInfo.MaximumAttempts,
			executionInfo.NonRetriableErrors,
			executionInfo.EventStoreVersion,
			executionInfo.BranchToken,
			executionInfo.CronSchedule,
			executionInfo.ExpirationSeconds,
			executionInfo.SearchAttributes,
			replicationState.CurrentVersion,
			replicationState.StartVersion,
			replicationState.LastWriteVersion,
			replicationState.LastWriteEventID,
			lastReplicationInfo,
			executionInfo.NextEventID,
			d.shardID,
			rowTypeExecution,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID)
	} else if versionHistories != nil {
		versionHistoriesData, versionHistoriesEncoding := p.FromDataBlob(versionHistories)
		batchQueryHelper(batch, templateUpdateWorkflowExecutionWithVersionHistoriesQuery, useCondition, condition,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			executionInfo.ParentDomainID,
			executionInfo.ParentWorkflowID,
			executionInfo.ParentRunID,
			executionInfo.InitiatedID,
			executionInfo.CompletionEventBatchID,
			completionData,
			completionEncoding,
			executionInfo.TaskList,
			executionInfo.WorkflowTypeName,
			executionInfo.WorkflowTimeout,
			executionInfo.DecisionTimeoutValue,
			executionInfo.ExecutionContext,
			executionInfo.State,
			executionInfo.CloseStatus,
			executionInfo.LastFirstEventID,
			executionInfo.LastEventTaskID,
			executionInfo.NextEventID,
			executionInfo.LastProcessedEvent,
			executionInfo.StartTimestamp,
			cqlNowTimestamp,
			executionInfo.CreateRequestID,
			executionInfo.SignalCount,
			executionInfo.HistorySize,
			executionInfo.DecisionVersion,
			executionInfo.DecisionScheduleID,
			executionInfo.DecisionStartedID,
			executionInfo.DecisionRequestID,
			executionInfo.DecisionTimeout,
			executionInfo.DecisionAttempt,
			executionInfo.DecisionStartedTimestamp,
			executionInfo.DecisionScheduledTimestamp,
			executionInfo.CancelRequested,
			executionInfo.CancelRequestID,
			executionInfo.StickyTaskList,
			executionInfo.StickyScheduleToStartTimeout,
			executionInfo.ClientLibraryVersion,
			executionInfo.ClientFeatureVersion,
			executionInfo.ClientImpl,
			executionInfo.AutoResetPoints.Data,
			executionInfo.AutoResetPoints.GetEncoding(),
			executionInfo.Attempt,
			executionInfo.HasRetryPolicy,
			executionInfo.InitialInterval,
			executionInfo.BackoffCoefficient,
			executionInfo.MaximumInterval,
			executionInfo.ExpirationTime,
			executionInfo.MaximumAttempts,
			executionInfo.NonRetriableErrors,
			executionInfo.EventStoreVersion,
			executionInfo.BranchToken,
			executionInfo.CronSchedule,
			executionInfo.ExpirationSeconds,
			executionInfo.SearchAttributes,
			executionInfo.NextEventID,
			versionHistoriesData,
			versionHistoriesEncoding,
			d.shardID,
			rowTypeExecution,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID)
	} else {
		// Updates will be called with null ReplicationState while the feature is disabled
		batchQueryHelper(batch, templateUpdateWorkflowExecutionQuery, useCondition, condition,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			executionInfo.ParentDomainID,
			executionInfo.ParentWorkflowID,
			executionInfo.ParentRunID,
			executionInfo.InitiatedID,
			executionInfo.CompletionEventBatchID,
			completionData,
			completionEncoding,
			executionInfo.TaskList,
			executionInfo.WorkflowTypeName,
			executionInfo.WorkflowTimeout,
			executionInfo.DecisionTimeoutValue,
			executionInfo.ExecutionContext,
			executionInfo.State,
			executionInfo.CloseStatus,
			executionInfo.LastFirstEventID,
			executionInfo.LastEventTaskID,
			executionInfo.NextEventID,
			executionInfo.LastProcessedEvent,
			executionInfo.StartTimestamp,
			cqlNowTimestamp,
			executionInfo.CreateRequestID,
			executionInfo.SignalCount,
			executionInfo.HistorySize,
			executionInfo.DecisionVersion,
			executionInfo.DecisionScheduleID,
			executionInfo.DecisionStartedID,
			executionInfo.DecisionRequestID,
			executionInfo.DecisionTimeout,
			executionInfo.DecisionAttempt,
			executionInfo.DecisionStartedTimestamp,
			executionInfo.DecisionScheduledTimestamp,
			executionInfo.CancelRequested,
			executionInfo.CancelRequestID,
			executionInfo.StickyTaskList,
			executionInfo.StickyScheduleToStartTimeout,
			executionInfo.ClientLibraryVersion,
			executionInfo.ClientFeatureVersion,
			executionInfo.ClientImpl,
			executionInfo.AutoResetPoints.Data,
			executionInfo.AutoResetPoints.GetEncoding(),
			executionInfo.Attempt,
			executionInfo.HasRetryPolicy,
			executionInfo.InitialInterval,
			executionInfo.BackoffCoefficient,
			executionInfo.MaximumInterval,
			executionInfo.ExpirationTime,
			executionInfo.MaximumAttempts,
			executionInfo.NonRetriableErrors,
			executionInfo.EventStoreVersion,
			executionInfo.BranchToken,
			executionInfo.CronSchedule,
			executionInfo.ExpirationSeconds,
			executionInfo.SearchAttributes,
			executionInfo.NextEventID,
			d.shardID,
			rowTypeExecution,
			executionInfo.DomainID,
			executionInfo.WorkflowID,
			executionInfo.RunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID)
	}
}

func (d *cassandraPersistence) UpdateWorkflowExecution(request *p.InternalUpdateWorkflowExecutionRequest) error {
	batch := d.session.NewBatch(gocql.LoggedBatch)
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())

	updateWorkflow := request.UpdateWorkflowMutation
	executionInfo := updateWorkflow.ExecutionInfo

	if err := d.applyWorkflowMutationBatch(batch, &updateWorkflow); err != nil {
		return err
	}

	if request.ContinueAsNew != nil {
		startReq := request.ContinueAsNew
		if err := d.CreateWorkflowExecutionWithinBatch(startReq, batch, cqlNowTimestamp); err != nil {
			return err
		}
		d.createTransferTasks(batch, startReq.TransferTasks, startReq.DomainID, startReq.Execution.GetWorkflowId(),
			startReq.Execution.GetRunId())
		d.createTimerTasks(batch, startReq.TimerTasks, startReq.DomainID, startReq.Execution.GetWorkflowId(),
			startReq.Execution.GetRunId(), cqlNowTimestamp)
	} else {
		switch executionInfo.State {
		case p.WorkflowStateZombie:
			if err := d.assertNotCurrentExecution(
				executionInfo.DomainID,
				executionInfo.WorkflowID,
				executionInfo.RunID); err != nil {
				return err
			}
		case p.WorkflowStateCreated, p.WorkflowStateRunning, p.WorkflowStateCompleted:
			startVersion := common.EmptyVersion
			lastWriteVersion := common.EmptyVersion
			if updateWorkflow.ReplicationState != nil {
				startVersion = updateWorkflow.ReplicationState.StartVersion
				lastWriteVersion = updateWorkflow.ReplicationState.LastWriteVersion
			}
			batch.Query(templateUpdateCurrentWorkflowExecutionQuery,
				executionInfo.RunID,
				executionInfo.RunID,
				executionInfo.CreateRequestID,
				executionInfo.State,
				executionInfo.CloseStatus,
				startVersion,
				lastWriteVersion,
				lastWriteVersion,
				executionInfo.State,
				d.shardID,
				rowTypeExecution,
				executionInfo.DomainID,
				executionInfo.WorkflowID,
				permanentRunID,
				defaultVisibilityTimestamp,
				rowTypeExecutionTaskID,
				executionInfo.RunID,
			)
		default:
			return &workflow.InternalServiceError{
				Message: fmt.Sprintf("UpdateWorkflowExecution operation failed. Unknown workflow state %v", executionInfo.State),
			}
		}
	}

	// Verifies that the RangeID has not changed
	batch.Query(templateUpdateLeaseQuery,
		request.RangeID,
		d.shardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		request.RangeID,
	)

	previous := make(map[string]interface{})
	applied, iter, err := d.session.MapExecuteBatchCAS(batch, previous)
	defer func() {
		if iter != nil {
			iter.Close()
		}
	}()

	if err != nil {
		if isTimeoutError(err) {
			// Write may have succeeded, but we don't know
			// return this info to the caller so they have the option of trying to find out by executing a read
			return &p.TimeoutError{Msg: fmt.Sprintf("UpdateWorkflowExecution timed out. Error: %v", err)}
		} else if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("UpdateWorkflowExecution operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("UpdateWorkflowExecution operation failed. Error: %v", err),
		}
	}

	if !applied {
		return d.getExecutionConditionalUpdateFailure(previous, iter, executionInfo.RunID, updateWorkflow.Condition, request.RangeID, executionInfo.RunID)
	}
	return nil
}

//TODO: update query with version histories
func (d *cassandraPersistence) ResetWorkflowExecution(request *p.InternalResetWorkflowExecutionRequest) error {
	batch := d.session.NewBatch(gocql.LoggedBatch)
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())

	currExecutionInfo := request.CurrExecutionInfo
	currReplicationState := request.CurrReplicationState

	insertExecutionInfo := request.NewWorkflowSnapshot.ExecutionInfo
	insertReplicationState := request.NewWorkflowSnapshot.ReplicationState

	startVersion := common.EmptyVersion
	lastWriteVersion := common.EmptyVersion
	if insertReplicationState != nil {
		startVersion = insertReplicationState.StartVersion
		lastWriteVersion = insertReplicationState.LastWriteVersion
	}

	if currReplicationState == nil {
		batch.Query(templateUpdateCurrentWorkflowExecutionQuery,
			insertExecutionInfo.RunID,
			insertExecutionInfo.RunID,
			insertExecutionInfo.CreateRequestID,
			insertExecutionInfo.State,
			insertExecutionInfo.CloseStatus,
			startVersion,
			lastWriteVersion,
			lastWriteVersion,
			insertExecutionInfo.State,
			d.shardID,
			rowTypeExecution,
			insertExecutionInfo.DomainID,
			insertExecutionInfo.WorkflowID,
			permanentRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			currExecutionInfo.RunID,
		)
	} else {
		batch.Query(templateUpdateCurrentWorkflowExecutionForNewQuery,
			insertExecutionInfo.RunID,
			insertExecutionInfo.RunID,
			insertExecutionInfo.CreateRequestID,
			insertExecutionInfo.State,
			insertExecutionInfo.CloseStatus,
			startVersion,
			lastWriteVersion,
			lastWriteVersion,
			insertExecutionInfo.State,
			d.shardID,
			rowTypeExecution,
			insertExecutionInfo.DomainID,
			insertExecutionInfo.WorkflowID,
			permanentRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			currExecutionInfo.RunID,
			request.PrevRunVersion,
			request.PrevRunState,
		)
	}

	// for forkRun, check condition without updating anything to make sure the forkRun hasn't been deleted.
	// Without this check, it will run into race condition with deleteHistoryEvent timer task
	// we only do it when forkRun != currentRun
	if request.BaseRunID != currExecutionInfo.RunID {
		batch.Query(templateCheckWorkflowExecutionQuery,
			request.BaseRunNextEventID,
			d.shardID,
			rowTypeExecution,
			currExecutionInfo.DomainID,
			currExecutionInfo.WorkflowID,
			request.BaseRunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			request.BaseRunNextEventID,
		)
	}

	if request.UpdateCurr {
		d.updateMutableState(batch, currExecutionInfo, currReplicationState, request.CurrVersionHistories, cqlNowTimestamp, true, request.Condition)
		d.createTimerTasks(batch, request.CurrTimerTasks, currExecutionInfo.DomainID, currExecutionInfo.WorkflowID, currExecutionInfo.RunID, cqlNowTimestamp)
		d.createTransferTasks(batch, request.CurrTransferTasks, currExecutionInfo.DomainID, currExecutionInfo.WorkflowID, currExecutionInfo.RunID)
	} else {
		// check condition without updating anything
		batch.Query(templateCheckWorkflowExecutionQuery,
			request.Condition,
			d.shardID,
			rowTypeExecution,
			currExecutionInfo.DomainID,
			currExecutionInfo.WorkflowID,
			currExecutionInfo.RunID,
			defaultVisibilityTimestamp,
			rowTypeExecutionTaskID,
			request.Condition,
		)
	}

	if len(request.NewWorkflowSnapshot.ReplicationTasks) > 0 {
		d.createReplicationTasks(batch, request.NewWorkflowSnapshot.ReplicationTasks, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID, insertExecutionInfo.RunID)
	}
	if len(request.CurrReplicationTasks) > 0 {
		d.createReplicationTasks(batch, request.CurrReplicationTasks, currExecutionInfo.DomainID, currExecutionInfo.WorkflowID, currExecutionInfo.RunID)
	}

	// we need to insert new mutableState, there is no condition to check. We use update without condition as insert
	d.updateMutableState(batch, insertExecutionInfo, insertReplicationState, request.NewWorkflowSnapshot.VersionHistories, cqlNowTimestamp, false, 0)

	if len(request.NewWorkflowSnapshot.ActivityInfos) > 0 {
		if err := d.resetActivityInfos(batch, request.NewWorkflowSnapshot.ActivityInfos, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0); err != nil {
			return err
		}
	}

	if len(request.NewWorkflowSnapshot.TimerInfos) > 0 {
		d.resetTimerInfos(batch, request.NewWorkflowSnapshot.TimerInfos, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0)
	}

	if len(request.NewWorkflowSnapshot.RequestCancelInfos) > 0 {
		d.resetRequestCancelInfos(batch, request.NewWorkflowSnapshot.RequestCancelInfos, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0)
	}

	if len(request.NewWorkflowSnapshot.ChildExecutionInfos) > 0 {
		if err := d.resetChildExecutionInfos(batch, request.NewWorkflowSnapshot.ChildExecutionInfos, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0); err != nil {
			return err
		}
	}

	if len(request.NewWorkflowSnapshot.SignalInfos) > 0 {
		d.resetSignalInfos(batch, request.NewWorkflowSnapshot.SignalInfos, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0)
	}

	if len(request.NewWorkflowSnapshot.SignalRequestedIDs) > 0 {
		d.resetSignalRequested(batch, request.NewWorkflowSnapshot.SignalRequestedIDs, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID,
			insertExecutionInfo.RunID, false, 0)
	}

	if len(request.NewWorkflowSnapshot.TimerTasks) > 0 {
		d.createTimerTasks(batch, request.NewWorkflowSnapshot.TimerTasks, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID, insertExecutionInfo.RunID, cqlNowTimestamp)
	}

	if len(request.NewWorkflowSnapshot.TransferTasks) > 0 {
		d.createTransferTasks(batch, request.NewWorkflowSnapshot.TransferTasks, insertExecutionInfo.DomainID, insertExecutionInfo.WorkflowID, insertExecutionInfo.RunID)
	}

	//Verifies that the RangeID has not changed
	batch.Query(templateUpdateLeaseQuery,
		request.RangeID,
		d.shardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		request.RangeID,
	)

	previous := make(map[string]interface{})
	applied, iter, err := d.session.MapExecuteBatchCAS(batch, previous)
	defer func() {
		if iter != nil {
			iter.Close()
		}
	}()

	if err != nil {
		if isTimeoutError(err) {
			// Write may have succeeded, but we don't know
			// return this info to the caller so they have the option of trying to find out by executing a read
			return &p.TimeoutError{Msg: fmt.Sprintf("ResetWorkflowExecution timed out. Error: %v", err)}
		} else if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("ResetWorkflowExecution operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("ResetWorkflowExecution operation failed. Error: %v", err),
		}
	}

	if !applied {
		return d.getExecutionConditionalUpdateFailure(previous, iter, currExecutionInfo.RunID, request.Condition, request.RangeID, currExecutionInfo.RunID)
	}

	return nil
}

func (d *cassandraPersistence) ResetMutableState(request *p.InternalResetMutableStateRequest) error {
	batch := d.session.NewBatch(gocql.LoggedBatch)

	resetWorkflow := request.ResetWorkflowSnapshot
	executionInfo := resetWorkflow.ExecutionInfo
	replicationState := resetWorkflow.ReplicationState

	batch.Query(templateUpdateCurrentWorkflowExecutionForNewQuery,
		executionInfo.RunID,
		executionInfo.RunID,
		executionInfo.CreateRequestID,
		executionInfo.State,
		executionInfo.CloseStatus,
		replicationState.StartVersion,
		replicationState.LastWriteVersion,
		replicationState.LastWriteVersion,
		executionInfo.State,
		d.shardID,
		rowTypeExecution,
		executionInfo.DomainID,
		executionInfo.WorkflowID,
		permanentRunID,
		defaultVisibilityTimestamp,
		rowTypeExecutionTaskID,
		request.PrevRunID,
		request.PrevLastWriteVersion,
		request.PrevState,
	)

	if err := d.applyWorkflowSnapshotBatch(batch, &resetWorkflow); err != nil {
		return err
	}

	// Verifies that the RangeID has not changed
	batch.Query(templateUpdateLeaseQuery,
		request.RangeID,
		d.shardID,
		rowTypeShard,
		rowTypeShardDomainID,
		rowTypeShardWorkflowID,
		rowTypeShardRunID,
		defaultVisibilityTimestamp,
		rowTypeShardTaskID,
		request.RangeID,
	)

	previous := make(map[string]interface{})
	applied, iter, err := d.session.MapExecuteBatchCAS(batch, previous)
	defer func() {
		if iter != nil {
			iter.Close()
		}
	}()

	if err != nil {
		if isTimeoutError(err) {
			// Write may have succeeded, but we don't know
			// return this info to the caller so they have the option of trying to find out by executing a read
			return &p.TimeoutError{Msg: fmt.Sprintf("ResetMutableState timed out. Error: %v", err)}
		} else if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("ResetMutableState operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("ResetMutableState operation failed. Error: %v", err),
		}
	}

	if !applied {
		return d.getExecutionConditionalUpdateFailure(previous, iter, executionInfo.RunID, request.ResetWorkflowSnapshot.Condition, request.RangeID, request.PrevRunID)
	}

	return nil
}

func (d *cassandraPersistence) getExecutionConditionalUpdateFailure(previous map[string]interface{}, iter *gocql.Iter, requestRunID string, requestCondition int64, requestRangeID int64, requestConditionalRunID string) error {
	// There can be three reasons why the query does not get applied: the RangeID has changed, or the next_event_id or current_run_id check failed.
	// Check the row info returned by Cassandra to figure out which one it is.
	rangeIDUnmatch := false
	actualRangeID := int64(0)
	nextEventIDUnmatch := false
	actualNextEventID := int64(0)
	runIDUnmatch := false
	actualCurrRunID := ""
	allPrevious := []map[string]interface{}{}

GetFailureReasonLoop:
	for {
		rowType, ok := previous["type"].(int)
		if !ok {
			// This should never happen, as all our rows have the type field.
			break GetFailureReasonLoop
		}

		runID := previous["run_id"].(gocql.UUID).String()

		if rowType == rowTypeShard {
			if actualRangeID, ok = previous["range_id"].(int64); ok && actualRangeID != requestRangeID {
				// UpdateWorkflowExecution failed because rangeID was modified
				rangeIDUnmatch = true
			}
		} else if rowType == rowTypeExecution && runID == requestRunID {
			if actualNextEventID, ok = previous["next_event_id"].(int64); ok && actualNextEventID != requestCondition {
				// UpdateWorkflowExecution failed because next event ID is unexpected
				nextEventIDUnmatch = true
			}
		} else if rowType == rowTypeExecution && runID == permanentRunID {
			// UpdateWorkflowExecution failed because current_run_id is unexpected
			if actualCurrRunID = previous["current_run_id"].(gocql.UUID).String(); actualCurrRunID != requestConditionalRunID {
				// UpdateWorkflowExecution failed because next event ID is unexpected
				runIDUnmatch = true
			}
		}

		allPrevious = append(allPrevious, previous)
		previous = make(map[string]interface{})
		if !iter.MapScan(previous) {
			// Cassandra returns the actual row that caused a condition failure, so we should always return
			// from the checks above, but just in case.
			break GetFailureReasonLoop
		}
	}

	if rangeIDUnmatch {
		return &p.ShardOwnershipLostError{
			ShardID: d.shardID,
			Msg: fmt.Sprintf("Failed to update mutable state.  Request RangeID: %v, Actual RangeID: %v",
				requestRangeID, actualRangeID),
		}
	}

	if runIDUnmatch {
		return &p.CurrentWorkflowConditionFailedError{
			Msg: fmt.Sprintf("Failed to update mutable state.  Request Condition: %v, Actual Value: %v, Request Current RunID: %v, Actual Value: %v",
				requestCondition, actualNextEventID, requestConditionalRunID, actualCurrRunID),
		}
	}

	if nextEventIDUnmatch {
		return &p.ConditionFailedError{
			Msg: fmt.Sprintf("Failed to update mutable state.  Request Condition: %v, Actual Value: %v, Request Current RunID: %v, Actual Value: %v",
				requestCondition, actualNextEventID, requestConditionalRunID, actualCurrRunID),
		}
	}

	// At this point we only know that the write was not applied.
	var columns []string
	columnID := 0
	for _, previous := range allPrevious {
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%v: %s=%v", columnID, k, v))
		}
		columnID++
	}
	return &p.ConditionFailedError{
		Msg: fmt.Sprintf("Failed to reset mutable state. ShardID: %v, RangeID: %v, Condition: %v, Request Current RunID: %v, columns: (%v)",
			d.shardID, requestRangeID, requestCondition, requestConditionalRunID, strings.Join(columns, ",")),
	}
}

func (d *cassandraPersistence) DeleteWorkflowExecution(request *p.DeleteWorkflowExecutionRequest) error {
	query := d.session.Query(templateDeleteWorkflowExecutionMutableStateQuery,
		d.shardID,
		rowTypeExecution,
		request.DomainID,
		request.WorkflowID,
		request.RunID,
		defaultVisibilityTimestamp,
		rowTypeExecutionTaskID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("DeleteWorkflowExecution operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("DeleteWorkflowExecution operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) DeleteCurrentWorkflowExecution(request *p.DeleteCurrentWorkflowExecutionRequest) error {
	query := d.session.Query(templateDeleteWorkflowExecutionCurrentRowQuery,
		d.shardID,
		rowTypeExecution,
		request.DomainID,
		request.WorkflowID,
		permanentRunID,
		defaultVisibilityTimestamp,
		rowTypeExecutionTaskID,
		request.RunID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("DeleteWorkflowCurrentRow operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("DeleteWorkflowCurrentRow operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) GetCurrentExecution(request *p.GetCurrentExecutionRequest) (*p.GetCurrentExecutionResponse,
	error) {
	query := d.session.Query(templateGetCurrentExecutionQuery,
		d.shardID,
		rowTypeExecution,
		request.DomainID,
		request.WorkflowID,
		permanentRunID,
		defaultVisibilityTimestamp,
		rowTypeExecutionTaskID)

	result := make(map[string]interface{})
	if err := query.MapScan(result); err != nil {
		if err == gocql.ErrNotFound {
			return nil, &workflow.EntityNotExistsError{
				Message: fmt.Sprintf("Workflow execution not found.  WorkflowId: %v",
					request.WorkflowID),
			}
		} else if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("GetCurrentExecution operation failed. Error: %v", err),
			}
		}

		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetCurrentExecution operation failed. Error: %v", err),
		}
	}

	currentRunID := result["current_run_id"].(gocql.UUID).String()
	executionInfo := createWorkflowExecutionInfo(result["execution"].(map[string]interface{}))
	replicationState := createReplicationState(result["replication_state"].(map[string]interface{}))
	return &p.GetCurrentExecutionResponse{
		RunID:            currentRunID,
		StartRequestID:   executionInfo.CreateRequestID,
		State:            executionInfo.State,
		CloseStatus:      executionInfo.CloseStatus,
		LastWriteVersion: replicationState.LastWriteVersion,
	}, nil
}

func (d *cassandraPersistence) GetTransferTasks(request *p.GetTransferTasksRequest) (*p.GetTransferTasksResponse, error) {

	// Reading transfer tasks need to be quorum level consistent, otherwise we could loose task
	query := d.session.Query(templateGetTransferTasksQuery,
		d.shardID,
		rowTypeTransferTask,
		rowTypeTransferDomainID,
		rowTypeTransferWorkflowID,
		rowTypeTransferRunID,
		defaultVisibilityTimestamp,
		request.ReadLevel,
		request.MaxReadLevel,
	).PageSize(request.BatchSize).PageState(request.NextPageToken)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetTransferTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &p.GetTransferTasksResponse{}
	task := make(map[string]interface{})
	for iter.MapScan(task) {
		t := createTransferTaskInfo(task["transfer"].(map[string]interface{}))
		// Reset task map to get it ready for next scan
		task = make(map[string]interface{})

		response.Tasks = append(response.Tasks, t)
	}
	nextPageToken := iter.PageState()
	response.NextPageToken = make([]byte, len(nextPageToken))
	copy(response.NextPageToken, nextPageToken)

	if err := iter.Close(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetTransferTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}

func (d *cassandraPersistence) GetReplicationTasks(request *p.GetReplicationTasksRequest) (*p.GetReplicationTasksResponse,
	error) {

	// Reading replication tasks need to be quorum level consistent, otherwise we could loose task
	query := d.session.Query(templateGetReplicationTasksQuery,
		d.shardID,
		rowTypeReplicationTask,
		rowTypeReplicationDomainID,
		rowTypeReplicationWorkflowID,
		rowTypeReplicationRunID,
		defaultVisibilityTimestamp,
		request.ReadLevel,
		request.MaxReadLevel,
	).PageSize(request.BatchSize).PageState(request.NextPageToken)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetReplicationTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &p.GetReplicationTasksResponse{}
	task := make(map[string]interface{})
	for iter.MapScan(task) {
		t := createReplicationTaskInfo(task["replication"].(map[string]interface{}))
		// Reset task map to get it ready for next scan
		task = make(map[string]interface{})

		response.Tasks = append(response.Tasks, t)
	}
	nextPageToken := iter.PageState()
	response.NextPageToken = make([]byte, len(nextPageToken))
	copy(response.NextPageToken, nextPageToken)

	if err := iter.Close(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetReplicationTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}

func (d *cassandraPersistence) CompleteTransferTask(request *p.CompleteTransferTaskRequest) error {
	query := d.session.Query(templateCompleteTransferTaskQuery,
		d.shardID,
		rowTypeTransferTask,
		rowTypeTransferDomainID,
		rowTypeTransferWorkflowID,
		rowTypeTransferRunID,
		defaultVisibilityTimestamp,
		request.TaskID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CompleteTransferTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTransferTask operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) RangeCompleteTransferTask(request *p.RangeCompleteTransferTaskRequest) error {
	query := d.session.Query(templateRangeCompleteTransferTaskQuery,
		d.shardID,
		rowTypeTransferTask,
		rowTypeTransferDomainID,
		rowTypeTransferWorkflowID,
		rowTypeTransferRunID,
		defaultVisibilityTimestamp,
		request.ExclusiveBeginTaskID,
		request.InclusiveEndTaskID,
	)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("RangeCompleteTransferTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("RangeCompleteTransferTask operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) CompleteReplicationTask(request *p.CompleteReplicationTaskRequest) error {
	query := d.session.Query(templateCompleteTransferTaskQuery,
		d.shardID,
		rowTypeReplicationTask,
		rowTypeReplicationDomainID,
		rowTypeReplicationWorkflowID,
		rowTypeReplicationRunID,
		defaultVisibilityTimestamp,
		request.TaskID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CompleteReplicationTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteReplicationTask operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) CompleteTimerTask(request *p.CompleteTimerTaskRequest) error {
	ts := p.UnixNanoToDBTimestamp(request.VisibilityTimestamp.UnixNano())
	query := d.session.Query(templateCompleteTimerTaskQuery,
		d.shardID,
		rowTypeTimerTask,
		rowTypeTimerDomainID,
		rowTypeTimerWorkflowID,
		rowTypeTimerRunID,
		ts,
		request.TaskID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CompleteTimerTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTimerTask operation failed. Error: %v", err),
		}
	}

	return nil
}

func (d *cassandraPersistence) RangeCompleteTimerTask(request *p.RangeCompleteTimerTaskRequest) error {
	start := p.UnixNanoToDBTimestamp(request.InclusiveBeginTimestamp.UnixNano())
	end := p.UnixNanoToDBTimestamp(request.ExclusiveEndTimestamp.UnixNano())
	query := d.session.Query(templateRangeCompleteTimerTaskQuery,
		d.shardID,
		rowTypeTimerTask,
		rowTypeTimerDomainID,
		rowTypeTimerWorkflowID,
		rowTypeTimerRunID,
		start,
		end,
	)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("RangeCompleteTimerTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("RangeCompleteTimerTask operation failed. Error: %v", err),
		}
	}

	return nil
}

// From TaskManager interface
func (d *cassandraPersistence) LeaseTaskList(request *p.LeaseTaskListRequest) (*p.LeaseTaskListResponse, error) {
	if len(request.TaskList) == 0 {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("LeaseTaskList requires non empty task list"),
		}
	}
	now := time.Now()
	query := d.session.Query(templateGetTaskList,
		request.DomainID,
		request.TaskList,
		request.TaskType,
		rowTypeTaskList,
		taskListTaskID,
	)
	var rangeID, ackLevel int64
	var tlDB map[string]interface{}
	err := query.Scan(&rangeID, &tlDB)
	if err != nil {
		if err == gocql.ErrNotFound { // First time task list is used
			query = d.session.Query(templateInsertTaskListQuery,
				request.DomainID,
				request.TaskList,
				request.TaskType,
				rowTypeTaskList,
				taskListTaskID,
				initialRangeID,
				request.DomainID,
				request.TaskList,
				request.TaskType,
				0,
				request.TaskListKind,
				now,
			)
		} else if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("LeaseTaskList operation failed. TaskList: %v, TaskType: %v, Error: %v",
					request.TaskList, request.TaskType, err),
			}
		} else {
			return nil, &workflow.InternalServiceError{
				Message: fmt.Sprintf("LeaseTaskList operation failed. TaskList: %v, TaskType: %v, Error: %v",
					request.TaskList, request.TaskType, err),
			}
		}
	} else {
		// if request.RangeID is > 0, we are trying to renew an already existing
		// lease on the task list. If request.RangeID=0, we are trying to steal
		// the tasklist from its current owner
		if request.RangeID > 0 && request.RangeID != rangeID {
			return nil, &p.ConditionFailedError{
				Msg: fmt.Sprintf("leaseTaskList:renew failed: taskList:%v, taskListType:%v, haveRangeID:%v, gotRangeID:%v",
					request.TaskList, request.TaskType, request.RangeID, rangeID),
			}
		}
		ackLevel = tlDB["ack_level"].(int64)
		taskListKind := tlDB["kind"].(int)
		query = d.session.Query(templateUpdateTaskListQuery,
			rangeID+1,
			request.DomainID,
			&request.TaskList,
			request.TaskType,
			ackLevel,
			taskListKind,
			now,
			request.DomainID,
			&request.TaskList,
			request.TaskType,
			rowTypeTaskList,
			taskListTaskID,
			rangeID,
		)
	}
	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("LeaseTaskList operation failed. Error: %v", err),
			}
		}
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("LeaseTaskList operation failed. Error : %v", err),
		}
	}
	if !applied {
		previousRangeID := previous["range_id"]
		return nil, &p.ConditionFailedError{
			Msg: fmt.Sprintf("leaseTaskList: taskList:%v, taskListType:%v, haveRangeID:%v, gotRangeID:%v",
				request.TaskList, request.TaskType, rangeID, previousRangeID),
		}
	}
	tli := &p.TaskListInfo{
		DomainID:    request.DomainID,
		Name:        request.TaskList,
		TaskType:    request.TaskType,
		RangeID:     rangeID + 1,
		AckLevel:    ackLevel,
		Kind:        request.TaskListKind,
		LastUpdated: now,
	}
	return &p.LeaseTaskListResponse{TaskListInfo: tli}, nil
}

// From TaskManager interface
func (d *cassandraPersistence) UpdateTaskList(request *p.UpdateTaskListRequest) (*p.UpdateTaskListResponse, error) {
	tli := request.TaskListInfo

	if tli.Kind == p.TaskListKindSticky { // if task_list is sticky, then update with TTL
		query := d.session.Query(templateUpdateTaskListQueryWithTTL,
			tli.DomainID,
			&tli.Name,
			tli.TaskType,
			rowTypeTaskList,
			taskListTaskID,
			tli.RangeID,
			tli.DomainID,
			&tli.Name,
			tli.TaskType,
			tli.AckLevel,
			tli.Kind,
			time.Now(),
			stickyTaskListTTL,
		)
		err := query.Exec()
		if err != nil {
			if isThrottlingError(err) {
				return nil, &workflow.ServiceBusyError{
					Message: fmt.Sprintf("UpdateTaskList operation failed. Error: %v", err),
				}
			}
			return nil, &workflow.InternalServiceError{
				Message: fmt.Sprintf("UpdateTaskList operation failed. Error: %v", err),
			}
		}
		return &p.UpdateTaskListResponse{}, nil
	}

	query := d.session.Query(templateUpdateTaskListQuery,
		tli.RangeID,
		tli.DomainID,
		&tli.Name,
		tli.TaskType,
		tli.AckLevel,
		tli.Kind,
		time.Now(),
		tli.DomainID,
		&tli.Name,
		tli.TaskType,
		rowTypeTaskList,
		taskListTaskID,
		tli.RangeID,
	)

	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("UpdateTaskList operation failed. Error: %v", err),
			}
		}
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("UpdateTaskList operation failed. Error: %v", err),
		}
	}

	if !applied {
		var columns []string
		for k, v := range previous {
			columns = append(columns, fmt.Sprintf("%s=%v", k, v))
		}

		return nil, &p.ConditionFailedError{
			Msg: fmt.Sprintf("Failed to update task list. name: %v, type: %v, rangeID: %v, columns: (%v)",
				tli.Name, tli.TaskType, tli.RangeID, strings.Join(columns, ",")),
		}
	}

	return &p.UpdateTaskListResponse{}, nil
}

func (d *cassandraPersistence) ListTaskList(request *p.ListTaskListRequest) (*p.ListTaskListResponse, error) {
	return nil, &workflow.InternalServiceError{
		Message: fmt.Sprintf("unsupported operation"),
	}
}

func (d *cassandraPersistence) DeleteTaskList(request *p.DeleteTaskListRequest) error {
	query := d.session.Query(templateDeleteTaskListQuery,
		request.DomainID, request.TaskListName, request.TaskListType, rowTypeTaskList, taskListTaskID, request.RangeID)
	previous := make(map[string]interface{})
	applied, err := query.MapScanCAS(previous)
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("DeleteTaskList operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("DeleteTaskList operation failed. Error: %v", err),
		}
	}
	if !applied {
		return &p.ConditionFailedError{
			Msg: fmt.Sprintf("DeleteTaskList operation failed: expected_range_id=%v but found %+v", request.RangeID, previous),
		}
	}
	return nil
}

// From TaskManager interface
func (d *cassandraPersistence) CreateTasks(request *p.CreateTasksRequest) (*p.CreateTasksResponse, error) {
	batch := d.session.NewBatch(gocql.LoggedBatch)
	domainID := request.TaskListInfo.DomainID
	taskList := request.TaskListInfo.Name
	taskListType := request.TaskListInfo.TaskType
	taskListKind := request.TaskListInfo.Kind
	ackLevel := request.TaskListInfo.AckLevel
	cqlNowTimestamp := p.UnixNanoToDBTimestamp(time.Now().UnixNano())

	for _, task := range request.Tasks {
		scheduleID := task.Data.ScheduleID
		ttl := int64(task.Data.ScheduleToStartTimeout)
		if ttl <= 0 {
			batch.Query(templateCreateTaskQuery,
				domainID,
				taskList,
				taskListType,
				rowTypeTask,
				task.TaskID,
				domainID,
				task.Execution.GetWorkflowId(),
				task.Execution.GetRunId(),
				scheduleID,
				cqlNowTimestamp)
		} else {
			if ttl > maxCassandraTTL {
				ttl = maxCassandraTTL
			}
			batch.Query(templateCreateTaskWithTTLQuery,
				domainID,
				taskList,
				taskListType,
				rowTypeTask,
				task.TaskID,
				domainID,
				task.Execution.GetWorkflowId(),
				task.Execution.GetRunId(),
				scheduleID,
				cqlNowTimestamp,
				ttl)
		}
	}

	// The following query is used to ensure that range_id didn't change
	batch.Query(templateUpdateTaskListQuery,
		request.TaskListInfo.RangeID,
		domainID,
		taskList,
		taskListType,
		ackLevel,
		taskListKind,
		time.Now(),
		domainID,
		taskList,
		taskListType,
		rowTypeTaskList,
		taskListTaskID,
		request.TaskListInfo.RangeID,
	)

	previous := make(map[string]interface{})
	applied, _, err := d.session.MapExecuteBatchCAS(batch, previous)
	if err != nil {
		if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CreateTasks operation failed. Error: %v", err),
			}
		}
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("CreateTasks operation failed. Error : %v", err),
		}
	}
	if !applied {
		rangeID := previous["range_id"]
		return nil, &p.ConditionFailedError{
			Msg: fmt.Sprintf("Failed to create task. TaskList: %v, taskListType: %v, rangeID: %v, db rangeID: %v",
				taskList, taskListType, request.TaskListInfo.RangeID, rangeID),
		}
	}

	return &p.CreateTasksResponse{}, nil
}

// From TaskManager interface
func (d *cassandraPersistence) GetTasks(request *p.GetTasksRequest) (*p.GetTasksResponse, error) {
	if request.MaxReadLevel == nil {
		return nil, &workflow.InternalServiceError{
			Message: "getTasks: both readLevel and maxReadLevel MUST be specified for cassandra persistence",
		}
	}
	if request.ReadLevel > *request.MaxReadLevel {
		return &p.GetTasksResponse{}, nil
	}

	// Reading tasklist tasks need to be quorum level consistent, otherwise we could loose task
	query := d.session.Query(templateGetTasksQuery,
		request.DomainID,
		request.TaskList,
		request.TaskType,
		rowTypeTask,
		request.ReadLevel,
		*request.MaxReadLevel,
	).PageSize(request.BatchSize)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &p.GetTasksResponse{}
	task := make(map[string]interface{})
PopulateTasks:
	for iter.MapScan(task) {
		taskID, ok := task["task_id"]
		if !ok { // no tasks, but static column record returned
			continue
		}
		t := createTaskInfo(task["task"].(map[string]interface{}))
		t.TaskID = taskID.(int64)
		response.Tasks = append(response.Tasks, t)
		if len(response.Tasks) == request.BatchSize {
			break PopulateTasks
		}
		task = make(map[string]interface{}) // Reinitialize map as initialized fails on unmarshalling
	}

	if err := iter.Close(); err != nil {
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}

// From TaskManager interface
func (d *cassandraPersistence) CompleteTask(request *p.CompleteTaskRequest) error {
	tli := request.TaskList
	query := d.session.Query(templateCompleteTaskQuery,
		tli.DomainID,
		tli.Name,
		tli.TaskType,
		rowTypeTask,
		request.TaskID)

	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CompleteTask operation failed. Error: %v", err),
			}
		}
		return &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTask operation failed. Error: %v", err),
		}
	}

	return nil
}

// CompleteTasksLessThan deletes all tasks less than or equal to the given task id. This API ignores the
// Limit request parameter i.e. either all tasks leq the task_id will be deleted or an error will
// be returned to the caller
func (d *cassandraPersistence) CompleteTasksLessThan(request *p.CompleteTasksLessThanRequest) (int, error) {
	query := d.session.Query(templateCompleteTasksLessThanQuery,
		request.DomainID, request.TaskListName, request.TaskType, rowTypeTask, request.TaskID)
	err := query.Exec()
	if err != nil {
		if isThrottlingError(err) {
			return 0, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("CompleteTasksLessThan operation failed. Error: %v", err),
			}
		}
		return 0, &workflow.InternalServiceError{
			Message: fmt.Sprintf("CompleteTasksLessThan operation failed. Error: %v", err),
		}
	}
	return p.UnknownNumRowsAffected, nil
}

func (d *cassandraPersistence) GetTimerIndexTasks(request *p.GetTimerIndexTasksRequest) (*p.GetTimerIndexTasksResponse,
	error) {
	// Reading timer tasks need to be quorum level consistent, otherwise we could loose task
	minTimestamp := p.UnixNanoToDBTimestamp(request.MinTimestamp.UnixNano())
	maxTimestamp := p.UnixNanoToDBTimestamp(request.MaxTimestamp.UnixNano())
	query := d.session.Query(templateGetTimerTasksQuery,
		d.shardID,
		rowTypeTimerTask,
		rowTypeTimerDomainID,
		rowTypeTimerWorkflowID,
		rowTypeTimerRunID,
		minTimestamp,
		maxTimestamp,
	).PageSize(request.BatchSize).PageState(request.NextPageToken)

	iter := query.Iter()
	if iter == nil {
		return nil, &workflow.InternalServiceError{
			Message: "GetTimerTasks operation failed.  Not able to create query iterator.",
		}
	}

	response := &p.GetTimerIndexTasksResponse{}
	task := make(map[string]interface{})
	for iter.MapScan(task) {
		t := createTimerTaskInfo(task["timer"].(map[string]interface{}))
		// Reset task map to get it ready for next scan
		task = make(map[string]interface{})

		response.Timers = append(response.Timers, t)
	}
	nextPageToken := iter.PageState()
	response.NextPageToken = make([]byte, len(nextPageToken))
	copy(response.NextPageToken, nextPageToken)

	if err := iter.Close(); err != nil {
		if isThrottlingError(err) {
			return nil, &workflow.ServiceBusyError{
				Message: fmt.Sprintf("GetTimerTasks operation failed. Error: %v", err),
			}
		}
		return nil, &workflow.InternalServiceError{
			Message: fmt.Sprintf("GetTimerTasks operation failed. Error: %v", err),
		}
	}

	return response, nil
}
