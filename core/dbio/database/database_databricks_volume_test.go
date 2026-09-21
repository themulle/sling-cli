package database

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/flarco/g"
	"github.com/slingdata-io/sling-cli/core/dbio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumePutHTTP_ResumeSkip(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "part_0000.parquet")
	content := []byte("fake-parquet-content-for-testing-resume")
	err := os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	headCalled := int32(0)
	putCalled := int32(0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		if r.Method == http.MethodHead {
			atomic.AddInt32(&headCalled, 1)
			w.Header().Set("Content-Length", g.F("%d", len(content)))
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPut {
			atomic.AddInt32(&putCalled, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}))
	defer server.Close()

	conn := newTestDatabricksConn()
	conn.SetProp("host", server.URL)
	conn.SetProp("token", "test-token")

	volumePath := "/Volumes/catalog/schema/volume/table/part_0000.parquet"
	err = conn.volumePutHTTP(filePath, volumePath)
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&headCalled), "HEAD request should be called to check existence")
	assert.Equal(t, int32(0), atomic.LoadInt32(&putCalled), "PUT request should NOT be called when file already matches size")
}

func TestVolumePutHTTP_UploadSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "part_0001.parquet")
	content := []byte("new-data-chunk-for-volume")
	err := os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	headCalled := int32(0)
	putCalled := int32(0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token-abc", r.Header.Get("Authorization"))

		if r.Method == http.MethodHead {
			atomic.AddInt32(&headCalled, 1)
			// File does not exist yet
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method == http.MethodPut {
			atomic.AddInt32(&putCalled, 1)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusCreated)
			return
		}
	}))
	defer server.Close()

	conn := newTestDatabricksConn()
	conn.SetProp("host", server.URL)
	conn.SetProp("token", "token-abc")

	volumePath := "/Volumes/catalog/schema/volume/table/part_0001.parquet"
	err = conn.volumePutHTTP(filePath, volumePath)
	require.NoError(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&headCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&putCalled))
}

func TestVolumePutHTTP_RetryOn500(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "part_0002.parquet")
	content := []byte("retryable-content")
	err := os.WriteFile(filePath, content, 0644)
	require.NoError(t, err)

	putAttempts := int32(0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method == http.MethodPut {
			attempt := atomic.AddInt32(&putAttempts, 1)
			if attempt < 2 {
				// Fail first attempt with HTTP 503
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte("Databricks temporary error"))
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
	}))
	defer server.Close()

	conn := newTestDatabricksConn()
	conn.SetProp("host", server.URL)
	conn.SetProp("token", "token-retry")

	volumePath := "/Volumes/catalog/schema/volume/table/part_0002.parquet"
	err = conn.volumePutHTTP(filePath, volumePath)
	require.NoError(t, err)

	assert.Equal(t, int32(2), atomic.LoadInt32(&putAttempts), "Should have succeeded on 2nd attempt after retry")
}

func TestMergeFromVolumeTemplate(t *testing.T) {
	conn := &DatabricksConn{}
	conn.BaseConn.Type = dbio.TypeDbDatabricks
	conn.Init()

	tmpl := conn.template.Core["merge_from_volume_parquet"]
	require.NotEmpty(t, tmpl, "merge_from_volume_parquet template should exist")

	sql := g.R(
		tmpl,
		"tgt_table", "`catalog`.`schema`.`target_table`",
		"volume_path", "/Volumes/catalog/schema/volume/table",
		"src_fields", "`id`, `name`, `updated_at`",
		"src_tgt_pk_equal", "tgt.`id` = src.`id`",
		"set_fields", "`name` = src.`name`, `updated_at` = src.`updated_at`",
		"insert_fields", "`id`, `name`, `updated_at`",
		"insert_values", "src.`id`, src.`name`, src.`updated_at`",
	)

	assert.Contains(t, sql, "MERGE INTO `catalog`.`schema`.`target_table` tgt")
	assert.Contains(t, sql, "read_files('/Volumes/catalog/schema/volume/table', format => 'parquet')")
	assert.Contains(t, sql, "ON (tgt.`id` = src.`id`)")
	assert.Contains(t, sql, "WHEN MATCHED THEN UPDATE SET `name` = src.`name`")
	assert.Contains(t, sql, "WHEN NOT MATCHED THEN INSERT (`id`, `name`, `updated_at`) VALUES (src.`id`, src.`name`, src.`updated_at`)")
}

func TestCopyMethodAuto_Selection(t *testing.T) {
	conn := newTestDatabricksConn()
	conn.CopyMethod = "auto"
	conn.ZerobusEndpoint = "https://123.zerobus.cloud.databricks.com"

	assert.Equal(t, "auto", conn.CopyMethod)
	assert.NotEmpty(t, conn.ZerobusEndpoint)
}
