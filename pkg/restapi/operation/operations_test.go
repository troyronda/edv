/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package operation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"github.com/trustbloc/edge-core/pkg/log"
	"github.com/trustbloc/edge-core/pkg/log/mocklogger"
	"github.com/trustbloc/edge-core/pkg/storage"

	"github.com/trustbloc/edv/pkg/edvprovider"
	"github.com/trustbloc/edv/pkg/edvprovider/memedvprovider"
	"github.com/trustbloc/edv/pkg/restapi/messages"
	"github.com/trustbloc/edv/pkg/restapi/models"
)

const (
	testReferenceID = "testReferenceID"
	testVaultID     = "Sr7yHjomhn1aeaFnxREfRN"
	testInvalidURI  = "invalidURI"
	testValidURI    = "did:example:123456789"
	testKEKType     = "AesKeyWrappingKey2019"
	testHMACType    = "Sha256HmacKey2019"

	testDataVaultConfiguration = `{
  "sequence": 0,
  "controller": "` + testValidURI + `",
  "referenceId": "` + testReferenceID + `",
  "kek": {
    "id": "https://example.com/kms/12345",
    "type": "AesKeyWrappingKey2019"
  },
  "hmac": {
    "id": "https://example.com/kms/67891",
    "type": "Sha256HmacKey2019"
  }
}`

	testQuery = `{
  "index": "CUQaxPtSLtd8L3WBAIkJ4DiVJeqoF6bdnhR7lSaPloZ",
  "equals": "RV58Va4904K-18_L5g_vfARXRWEB00knFSGPpukUBro"
}`
	testQueryWithReturnFullDocuments = `{
  "returnFullDocuments": true,
  "index": "CUQaxPtSLtd8L3WBAIkJ4DiVJeqoF6bdnhR7lSaPloZ",
  "equals": "RV58Va4904K-18_L5g_vfARXRWEB00knFSGPpukUBro"
}`

	testDocID      = "VJYHHJx4C8J9Fsgz7rZqSp"
	testDocID2     = "AJYHHJx4C8J9Fsgz7rZqSp"
	testIndexName1 = "indexName1"
	testIndexName2 = "indexName2"
	testIndexName3 = "indexName3"

	testJWE1 = `{"protected":"eyJlbmMiOiJDMjBQIn0","recipients":[{"header":{"alg":"A256KW","kid":"https://exam` +
		`ple.com/kms/z7BgF536GaR"},"encrypted_key":"OR1vdCNvf_B68mfUxFQVT-vyXVrBembuiM40mAAjDC1-Qu5iArDbug"}],` +
		`"iv":"i8Nins2vTI3PlrYW","ciphertext":"Cb-963UCXblINT8F6MDHzMJN9EAhK3I","tag":"pfZO0JulJcrc3trOZy8rjA"}`
	testJWE2 = `{"protected":"eyJhbGciOiJSU0EtT0FFUCIsImVuYyI6IkEyNTZHQ00ifQ","encrypted_k` +
		`ey":"OKOawDo13gRp2ojaHV7LFpZcgV7T6DVZKTyKOMTYUmKoTCVJRgckCL9kiMT03JGeipsEdY3mx_etLbbWSrFr05kLzcSr4qKA` +
		`q7YN7e9jwQRb23nfa6c9d-StnImGyFDbSv04uVuxIp5Zms1gNxKKK2Da14B8S4rzVRltdYwam_lDp5XnZAYpQdb76FdIKLaVmqgfw` +
		`X7XWRxv2322i-vDxRfqNzo_tETKzpVLzfiwQyeyPGLBIO56YJ7eObdv0je81860ppamavo35UgoRdbYaBcoh9QcfylQr66oc6vFWX` +
		`RcZ_ZT2LawVCWTIy3brGPi6UklfCpIMfIjf7iGdXKHzg","iv":"48V1_ALb6US04U3b","ciphertext":"5eym8TW_c8SuK0ltJ` +
		`3rpYIzOeDQz7TALvtu6UG9oMo4vpzs9tX_EFShS8iB7j6jiSdiwkIr3ajwQzaBtQD_A","tag":"XFBoMYUZodetZdvTiFvSkQ"}`

	testIndexedAttributeCollections1 = `[{"sequence":0,"hmac":{"id":"","type":""},"attributes":[{"name":"` +
		testIndexName1 + `","value":"testVal","unique":true},{"name":"` + testIndexName2 +
		`","value":"testVal","unique":true}]}]`
	testIndexedAttributeCollections2 = `[{"sequence":0,"hmac":{"id":"","type":""},"attributes":[{"name":"` +
		testIndexName2 + `","value":"testVal","unique":true},{"name":"` + testIndexName3 +
		`","value":"testVal","unique":true}]}]`

	testEncryptedDocument = `{"id":"` + testDocID + `","sequence":0,"indexed":null,` +
		`"jwe":` + testJWE1 + `}`
	testEncryptedDocument2 = `{"id":"` + testDocID2 + `","sequence":0,"indexed":null,` +
		`"jwe":` + testJWE2 + `}`

	// All of the characters in the ID below are NOT in the base58 alphabet, so this ID is not base58 encoded
	testEncryptedDocumentWithNonBase58ID = `{
  "id": "0OIl"
}`

	testEncryptedDocumentWithIDThatWasNot128BitsBeforeBase58Encoding = `{
  "id": "2CHi6"
}`

	testEncryptedDocumentWithNoJWE = `{
	"id": "BJYHHJx4C8J9Fsgz7rZqSa"
}`
)

var (
	mockLoggerProvider       = mocklogger.Provider{MockLogger: &mocklogger.MockLogger{}} //nolint: gochecknoglobals
	errFailingResponseWriter = errors.New("failingResponseWriter always fails")
	errFailingReadCloser     = errors.New("failingReadCloser always fails")
)

type failingResponseWriter struct {
}

func (f failingResponseWriter) Header() http.Header {
	return nil
}

func (f failingResponseWriter) Write([]byte) (int, error) {
	return 0, errFailingResponseWriter
}

func (f failingResponseWriter) WriteHeader(int) {
}

type failingReadCloser struct{}

func (m failingReadCloser) Read([]byte) (n int, err error) {
	return 0, errFailingReadCloser
}

func (m failingReadCloser) Close() error {
	return nil
}

type mockContext struct {
	valueToReturnWhenValueMethodCalled interface{}
}

func (m mockContext) Deadline() (deadline time.Time, ok bool) {
	panic("implement me")
}

func (m mockContext) Done() <-chan struct{} {
	panic("implement me")
}

func (m mockContext) Err() error {
	panic("implement me")
}

func (m mockContext) Value(interface{}) interface{} {
	return m.valueToReturnWhenValueMethodCalled
}

type mockEDVProvider struct {
	errStoreCreateEDVIndex             error
	errStorePut                        error
	errStoreUpsertBulk                 error
	errStoreGet                        error
	errStoreGetAll                     error
	errStoreUpdate                     error
	errStoreDelete                     error
	errStoreStoreDataVaultConfig       error
	errStoreFindVaultIDVaultNamePair   error
	errCreateStore                     error
	errOpenStore                       error
	numTimesOpenStoreCalled            int
	numTimesOpenStoreCalledBeforeErr   int
	numTimesCreateStoreCalled          int
	numTimesCreateStoreCalledBeforeErr int
}

func (m *mockEDVProvider) CreateStore(string) error {
	if m.numTimesCreateStoreCalled == m.numTimesCreateStoreCalledBeforeErr {
		return m.errCreateStore
	}

	m.numTimesCreateStoreCalled++

	return nil
}

func (m *mockEDVProvider) OpenStore(string) (edvprovider.EDVStore, error) {
	if m.numTimesOpenStoreCalled == m.numTimesOpenStoreCalledBeforeErr {
		return nil, m.errOpenStore
	}

	m.numTimesOpenStoreCalled++

	return &mockEDVStore{
		errCreateEDVIndex:           m.errStoreCreateEDVIndex,
		errPut:                      m.errStorePut,
		errUpsertBulk:               m.errStoreUpsertBulk,
		errGet:                      m.errStoreGet,
		errGetAll:                   m.errStoreGetAll,
		errUpdate:                   m.errStoreUpdate,
		errDelete:                   m.errStoreDelete,
		errStoreDataVaultConfig:     m.errStoreStoreDataVaultConfig,
		errFindVaultIDVaultNamePair: m.errStoreFindVaultIDVaultNamePair,
	}, nil
}

type mockEDVStore struct {
	errCreateEDVIndex           error
	errPut                      error
	errUpsertBulk               error
	errGet                      error
	errGetAll                   error
	errUpdate                   error
	errDelete                   error
	errStoreDataVaultConfig     error
	errFindVaultIDVaultNamePair error
}

func (m *mockEDVStore) Put(models.EncryptedDocument) error {
	return m.errPut
}

func (m *mockEDVStore) UpsertBulk([]models.EncryptedDocument) error {
	return m.errUpsertBulk
}

func (m *mockEDVStore) GetAll() ([][]byte, error) {
	return nil, m.errGetAll
}

func (m *mockEDVStore) Get(string) ([]byte, error) {
	return nil, m.errGet
}

func (m *mockEDVStore) CreateEDVIndex() error {
	return m.errCreateEDVIndex
}

func (m *mockEDVStore) Query(*models.Query) ([]models.EncryptedDocument, error) {
	encryptedDoc1 := models.EncryptedDocument{ID: "docID1"}
	encryptedDoc2 := models.EncryptedDocument{ID: "docID2"}

	return []models.EncryptedDocument{encryptedDoc1, encryptedDoc2}, nil
}

func (m *mockEDVStore) Update(document models.EncryptedDocument) error {
	return m.errUpdate
}

func (m *mockEDVStore) Delete(docID string) error {
	return m.errDelete
}

func (m *mockEDVStore) CreateReferenceIDIndex() error {
	panic("implement me")
}

func (m *mockEDVStore) CreateEncryptedDocIDIndex() error {
	return nil
}

func (m *mockEDVStore) StoreDataVaultConfiguration(*models.DataVaultConfiguration, string) error {
	return m.errStoreDataVaultConfig
}

func TestMain(m *testing.M) {
	log.Initialize(&mockLoggerProvider)

	log.SetLevel(logModuleName, log.DEBUG)

	os.Exit(m.Run())
}

func TestNew(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		o := New(&Config{Provider: memedvprovider.NewProvider()})
		require.NotNil(t, o)
	})
}

func TestCreateDataVault(t *testing.T) {
	testValidateIncomingDataVaultConfiguration(t)

	t.Run("Success: without prefix", func(t *testing.T) {
		op := New(&Config{
			Provider: memedvprovider.NewProvider(), AuthEnable: true,
			AuthService: &mockAuthService{createValue: []byte("authData")},
		})

		createConfigStoreExpectSuccess(t, op)

		_, resp := createDataVaultExpectSuccess(t, op)

		require.Equal(t, string(resp), "authData")
	})

	t.Run("error from creating auth payload", func(t *testing.T) {
		op := New(&Config{
			Provider: memedvprovider.NewProvider(), AuthEnable: true,
			AuthService: &mockAuthService{createErr: fmt.Errorf("failed to create auth")},
		})

		createConfigStoreExpectSuccess(t, op)

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "failed to create auth")
	})

	t.Run("Invalid Data Vault Configuration JSON", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createVaultHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.InvalidVaultConfig, "unexpected end of JSON input"),
			rr.Body.String())
	})
	t.Run("Config store does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.VaultCreationFailure,
			fmt.Sprintf(messages.StoreVaultConfigFailure, messages.ConfigStoreNotFound)), rr.Body.String())
		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
	t.Run("Response writer fails while writing request read error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		op.createDataVaultHandler(failingResponseWriter{}, &http.Request{Body: failingReadCloser{}})

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.CreateVaultFailReadRequestBody+messages.FailWriteResponse,
				errFailingReadCloser, errFailingResponseWriter))
	})
	t.Run("Error when creating new store: duplicate data vault from duplicate referenceID", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		createDataVaultExpectSuccess(t, op)

		rr := httptest.NewRecorder()
		op.createDataVault(rr, &models.DataVaultConfiguration{ReferenceID: testReferenceID}, "",
			nil)
		require.Equal(t, http.StatusConflict, rr.Code)
		require.Equal(t, "Failed to create a new data vault: failed to store data vault configuration: "+
			"an error occurred while querying reference IDs: vault already exists.", rr.Body.String())
	})
	t.Run("Other error when creating new store", func(t *testing.T) {
		errTest := errors.New("some other create store error")
		op := New(&Config{Provider: &mockEDVProvider{
			errCreateStore: errTest, numTimesOpenStoreCalledBeforeErr: 1,
			numTimesCreateStoreCalledBeforeErr: 0,
		}})

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.VaultCreationFailure, errTest), rr.Body.String())
	})
	t.Run("Response writer fails while writing duplicate data vault error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		createDataVaultExpectSuccess(t, op)

		op.createDataVault(&failingResponseWriter{},
			&models.DataVaultConfiguration{ReferenceID: testReferenceID}, "", nil)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.VaultCreationFailure+messages.FailWriteResponse,
				fmt.Sprintf(messages.StoreVaultConfigFailure,
					fmt.Sprintf(messages.CheckDuplicateRefIDFailure, messages.ErrDuplicateVault)),
				errFailingResponseWriter))
	})
	t.Run("Fail to store data vault configuration", func(t *testing.T) {
		errTest := errors.New("store data vault config error")
		op := New(&Config{Provider: &mockEDVProvider{
			errStoreStoreDataVaultConfig:     errTest,
			numTimesOpenStoreCalledBeforeErr: 1,
		}})

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.VaultCreationFailure,
			fmt.Sprintf(messages.StoreVaultConfigFailure, errTest)), rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Fail to store data vault configuration - config vault not found", func(t *testing.T) {
		op := New(&Config{Provider: &mockEDVProvider{
			errOpenStore:                     errors.New(messages.ConfigStoreNotFound),
			numTimesOpenStoreCalledBeforeErr: 0,
		}})

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.VaultCreationFailure,
			fmt.Sprintf(messages.StoreVaultConfigFailure, messages.ConfigStoreNotFound)), rr.Body.String())
		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
	t.Run("Fail to create EDV index", func(t *testing.T) {
		errTest := errors.New("create EDV index error")
		op := New(&Config{Provider: &mockEDVProvider{
			errStoreCreateEDVIndex:           errTest,
			numTimesOpenStoreCalledBeforeErr: 2,
		}})

		req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
		createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.VaultCreationFailure, errTest), rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func testValidateIncomingDataVaultConfiguration(t *testing.T) {
	t.Run("Invalid incoming data vault configuration - missing controller", func(t *testing.T) {
		config := getDataVaultConfig("", testValidURI, testKEKType, testValidURI,
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config, fmt.Sprintf(messages.InvalidVaultConfig, messages.BlankController))
	})
	t.Run("Invalid incoming data vault configuration - missing KEK ID", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, "", testKEKType, testValidURI,
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config, fmt.Sprintf(messages.InvalidVaultConfig, messages.BlankKEKID))
	})
	t.Run("Invalid incoming data vault configuration - missing KEK type", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testValidURI, "", testValidURI,
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config, fmt.Sprintf(messages.InvalidVaultConfig, messages.BlankKEKType))
	})
	t.Run("Invalid incoming data vault configuration - missing HMAC ID", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testValidURI, testKEKType, "",
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config, fmt.Sprintf(messages.InvalidVaultConfig, messages.BlankHMACID))
	})
	t.Run("Invalid incoming data vault configuration - missing HMAC type", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testValidURI, testKEKType, testValidURI,
			"", []string{}, []string{})
		createDataVaultExpectError(t, config, fmt.Sprintf(messages.InvalidVaultConfig, messages.BlankHMACType))
	})
	t.Run("Invalid incoming data vault configuration - controller is an invalid URI", func(t *testing.T) {
		config := getDataVaultConfig(testInvalidURI, testValidURI, testKEKType, testValidURI,
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config,
			fmt.Sprintf(messages.InvalidVaultConfig, fmt.Errorf(messages.InvalidControllerString,
				fmt.Errorf(messages.InvalidURI, testInvalidURI))))
	})
	t.Run("Invalid incoming data vault configuration - KEK id is an invalid URI", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testInvalidURI, testKEKType, testValidURI,
			testHMACType, []string{}, []string{})
		createDataVaultExpectError(t, config,
			fmt.Sprintf(messages.InvalidVaultConfig, fmt.Errorf(messages.InvalidKEKIDString,
				fmt.Errorf(messages.InvalidURI, testInvalidURI))))
	})
	t.Run("Invalid incoming data vault configuration - invoker contains invalid URIs", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testValidURI, testKEKType, testValidURI,
			testHMACType, []string{}, []string{testInvalidURI})
		createDataVaultExpectError(t, config,
			fmt.Sprintf(messages.InvalidVaultConfig, fmt.Errorf(messages.InvalidInvokerStringArray,
				fmt.Errorf(messages.InvalidURI, testInvalidURI))))
	})
	t.Run("Invalid incoming data vault configuration - delegator contains invalid URIs", func(t *testing.T) {
		config := getDataVaultConfig(testValidURI, testValidURI, testKEKType, testValidURI,
			testHMACType, []string{testInvalidURI}, []string{})
		createDataVaultExpectError(t, config,
			fmt.Sprintf(messages.InvalidVaultConfig, fmt.Errorf(messages.InvalidDelegatorStringArray,
				fmt.Errorf(messages.InvalidURI, testInvalidURI))))
	})
}

func TestQueryVault(t *testing.T) {
	t.Run("Success, returning document IDs", func(t *testing.T) {
		op := New(&Config{Provider: &mockEDVProvider{numTimesOpenStoreCalledBeforeErr: 4}})

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, `["/encrypted-data-vaults/`+vaultID+`/documents/`+
			`docID1","/encrypted-data-vaults/`+vaultID+`/documents/docID2"]`,
			rr.Body.String())
		require.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("Success, returning full documents", func(t *testing.T) {
		op := New(&Config{
			Provider:          &mockEDVProvider{numTimesOpenStoreCalledBeforeErr: 4},
			EnabledExtensions: &EnabledExtensions{ReturnFullDocumentsOnQuery: true},
		})

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQueryWithReturnFullDocuments)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		docsBytes := rr.Body.Bytes()

		var docs []models.EncryptedDocument

		err = json.Unmarshal(docsBytes, &docs)
		require.NoError(t, err)

		require.Equal(t, "docID1", docs[0].ID)
		require.Equal(t, "docID2", docs[1].ID)
		require.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("Provider doesn't support querying", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.QueryFailure, vaultID, memedvprovider.ErrQueryingNotSupported),
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Error: vault not found", func(t *testing.T) {
		op := New(&Config{Provider: &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 2, errOpenStore: storage.ErrStoreNotFound,
		}})

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.QueryFailure, vaultID, messages.ErrVaultNotFound), rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Error: fail to open store", func(t *testing.T) {
		testErr := errors.New("fail to open store")
		op := New(&Config{Provider: &mockEDVProvider{numTimesOpenStoreCalledBeforeErr: 2, errOpenStore: testErr}})

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.QueryFailure, vaultID, testErr), rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Error when writing response after an error happens while querying vault", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(failingResponseWriter{}, req)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.QueryFailure+messages.FailWriteResponse, vaultID,
				memedvprovider.ErrQueryingNotSupported, errFailingResponseWriter))
	})
	t.Run("Unable to unmarshal query JSON", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, fmt.Sprintf(messages.InvalidQuery, testVaultID, "unexpected end of JSON input"),
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Fail to write response when unable to unmarshal query JSON", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		req, err := http.NewRequest("POST", "", failingReadCloser{})
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		op.queryVaultHandler(failingResponseWriter{}, req)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.QueryFailReadRequestBody+messages.FailWriteResponse,
				testVaultID, errFailingReadCloser, errFailingResponseWriter))
	})
	t.Run("Fail to unescape path var", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testQuery)))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = "%"

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		queryVaultEndpointHandler := getHandler(t, op, queryVaultEndpoint, http.MethodPost)
		queryVaultEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t,
			fmt.Sprintf(messages.UnescapeFailure, vaultIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Fail to write response when matching documents are found (only IDs returned)", func(t *testing.T) {
		encryptedDoc1 := models.EncryptedDocument{ID: "docID1"}
		encryptedDoc2 := models.EncryptedDocument{ID: "docID2"}

		writeQueryResponse(failingResponseWriter{}, []models.EncryptedDocument{encryptedDoc1, encryptedDoc2},
			testVaultID, nil, false, "TestHost")

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.QuerySuccess+messages.FailWriteResponse, testVaultID, errFailingResponseWriter))
	})
	t.Run("Fail to write response when matching documents are found (full docs returned)", func(t *testing.T) {
		encryptedDoc1 := models.EncryptedDocument{ID: "docID1"}
		encryptedDoc2 := models.EncryptedDocument{ID: "docID2"}

		writeQueryResponse(failingResponseWriter{}, []models.EncryptedDocument{encryptedDoc1, encryptedDoc2},
			testVaultID, nil, true, "TestHost")

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.QuerySuccess+messages.FailWriteResponse, testVaultID, errFailingResponseWriter))
	})
}

func TestCreateDocument(t *testing.T) {
	t.Run("Success: without prefix", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)
		storeEncryptedDocumentExpectSuccess(t, op, testDocID2, testEncryptedDocument2, vaultID)
	})
	t.Run("Invalid encrypted document JSON", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.InvalidDocumentForDocCreation, testVaultID, "unexpected end of JSON input"),
			rr.Body.String())
	})
	t.Run("Document ID is not base58 encoded", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testEncryptedDocumentWithNonBase58ID)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.InvalidDocumentForDocCreation, vaultID, messages.ErrNotBase58Encoded),
			rr.Body.String())
	})
	t.Run("Document ID was not 128 bits long before being base58 encoded", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "",
			bytes.NewBuffer([]byte(testEncryptedDocumentWithIDThatWasNot128BitsBeforeBase58Encoding)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.InvalidDocumentForDocCreation, vaultID, messages.ErrNot128BitValue),
			rr.Body.String())
	})
	t.Run("Empty JWE", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "",
			bytes.NewBuffer([]byte(testEncryptedDocumentWithNoJWE)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.InvalidDocumentForDocCreation, vaultID,
			fmt.Sprintf(messages.InvalidRawJWE, messages.BlankJWE)), rr.Body.String())
	})
	t.Run("Duplicate document", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testEncryptedDocument)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)
		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusConflict, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.CreateDocumentFailure, vaultID, messages.ErrDuplicateDocument),
			rr.Body.String())
	})
	t.Run("Response writer fails while writing duplicate document error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		op.createDocument(&failingResponseWriter{}, []byte(testEncryptedDocument), "", vaultID)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.CreateDocumentFailure+messages.FailWriteResponse,
				vaultID, messages.ErrDuplicateDocument, errFailingResponseWriter))
	})
	t.Run("Vault does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testEncryptedDocument)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.CreateDocumentFailure, testVaultID, messages.ErrVaultNotFound),
			rr.Body.String())
	})
	t.Run("Unable to escape vault ID path variable", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(testEncryptedDocument)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = "%"

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, "", rr.Header().Get("Location"))
		require.Equal(t,
			fmt.Sprintf(messages.UnescapeFailure, vaultIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
	})
	t.Run("Response writer fails while writing unescape Vault ID error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		createDataVaultExpectSuccess(t, op)

		request := http.Request{}

		op.createDocumentHandler(failingResponseWriter{},
			request.WithContext(mockContext{valueToReturnWhenValueMethodCalled: getMapWithVaultIDThatCannotBeEscaped()}))

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.UnescapeFailure+messages.FailWriteResponse, vaultIDPathVariable,
				errFailingResponseWriter, errFailingResponseWriter))
	})
	t.Run("Response writer fails while writing request read error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", failingReadCloser{})
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		op.createDocumentHandler(failingResponseWriter{}, req)

		require.Contains(t,
			mockLoggerProvider.MockLogger.AllLogContents, fmt.Sprintf(
				messages.CreateDocumentFailReadRequestBody+messages.FailWriteResponse,
				testVaultID, errFailingReadCloser, errFailingResponseWriter))
	})
}

func TestReadAllDocuments(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		op := New(&Config{
			Provider:          memedvprovider.NewProvider(),
			EnabledExtensions: &EnabledExtensions{ReadAllDocumentsEndpoint: true},
		})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)
		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)
		storeEncryptedDocumentExpectSuccess(t, op, testDocID2, testEncryptedDocument2, vaultID)

		readAllDocumentsEndpointHandler := getHandler(t, op, readAllDocumentsEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID

		req = mux.SetURLVars(req, urlVars)

		readAllDocumentsEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		var actualDocs []models.EncryptedDocument

		err = json.Unmarshal(rr.Body.Bytes(), &actualDocs)
		require.NoError(t, err)

		// Marshal to bytes so that we can compare with the expected docs easily
		actualDocumentsBytes1, err := json.Marshal(actualDocs[0])
		require.NoError(t, err)

		actualDocumentsBytes2, err := json.Marshal(actualDocs[1])
		require.NoError(t, err)

		var gotExpectedDocs bool

		// The order of the returned docs can vary - either order is acceptable
		if string(actualDocumentsBytes1) == testEncryptedDocument &&
			string(actualDocumentsBytes2) == testEncryptedDocument2 {
			gotExpectedDocs = true
		} else if string(actualDocumentsBytes1) == testEncryptedDocument2 &&
			string(actualDocumentsBytes2) == testEncryptedDocument {
			gotExpectedDocs = true
		}

		require.True(t, gotExpectedDocs, `Expected these two documents (in any order):
Expected document 1: %s

Expected document 2: %s

Actual document 1: %s
Actual document 2: %s`, testEncryptedDocument, testEncryptedDocument2,
			actualDocumentsBytes1, actualDocumentsBytes2)
	})
	t.Run("Vault does not exist", func(t *testing.T) {
		op := New(&Config{
			Provider:          memedvprovider.NewProvider(),
			EnabledExtensions: &EnabledExtensions{ReadAllDocumentsEndpoint: true},
		})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		readAllDocumentsEndpointHandler := getHandler(t, op, readAllDocumentsEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		readAllDocumentsEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusNotFound, rr.Code)

		require.Equal(t, fmt.Sprintf(messages.ReadAllDocumentsFailure, testVaultID, messages.ErrVaultNotFound),
			rr.Body.String())
	})
	t.Run("Error while getting all docs from store", func(t *testing.T) {
		errGetAll := errors.New("some get all error")
		op := New(&Config{
			Provider: &mockEDVProvider{
				numTimesOpenStoreCalledBeforeErr: 2,
				errStoreGetAll:                   errGetAll,
			},
			EnabledExtensions: &EnabledExtensions{ReadAllDocumentsEndpoint: true},
		})

		readAllDocumentsEndpointHandler := getHandler(t, op, readAllDocumentsEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		readAllDocumentsEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusInternalServerError, rr.Code)

		require.Equal(t, fmt.Sprintf(messages.ReadAllDocumentsFailure,
			testVaultID, fmt.Errorf(messages.FailWhileGetAllDocsFromStoreErrMsg, errGetAll).Error()),
			rr.Body.String())
	})
	t.Run("Unable to escape vault ID path variable", func(t *testing.T) {
		op := New(&Config{
			Provider:          memedvprovider.NewProvider(),
			EnabledExtensions: &EnabledExtensions{ReadAllDocumentsEndpoint: true},
		})

		readAllDocumentsEndpointHandler := getHandler(t, op, readAllDocumentsEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = "%"

		req = mux.SetURLVars(req, urlVars)

		readAllDocumentsEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)

		require.Equal(t, fmt.Sprintf(messages.UnescapeFailure, vaultIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
	})
}

func TestReadDocument(t *testing.T) {
	t.Run("Success: without prefix", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		readDocumentExpectSuccess(t, op)
	})
	t.Run("Vault does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)
		storeSampleConfigExpectSuccess(t, op)

		readDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)

		readDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.ReadDocumentFailure, testDocID, testVaultID, messages.ErrVaultNotFound),
			rr.Body.String())
	})
	t.Run("Document does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		readDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)

		readDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.ReadDocumentFailure,
			testDocID, vaultID, messages.ErrDocumentNotFound), rr.Body.String())
	})
	t.Run("Unable to escape vault ID path variable", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		readDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = "%"
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)

		readDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)

		require.Equal(t, fmt.Sprintf(messages.UnescapeFailure, vaultIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
	})
	t.Run("Unable to escape document ID path variable", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		readDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID
		urlVars[docIDPathVariable] = "%"

		req = mux.SetURLVars(req, urlVars)

		readDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)

		require.Equal(t, fmt.Sprintf(messages.UnescapeFailure, docIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
	})
	t.Run("Response writer fails while writing unescape vault ID error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		request := http.Request{}

		op.readDocumentHandler(failingResponseWriter{},
			request.WithContext(mockContext{valueToReturnWhenValueMethodCalled: getMapWithVaultIDThatCannotBeEscaped()}))

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.UnescapeFailure+messages.FailWriteResponse,
				vaultIDPathVariable, errFailingResponseWriter, errFailingResponseWriter))
	})
	t.Run("Response writer fails while writing unescape document ID error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		request := http.Request{}

		op.readDocumentHandler(failingResponseWriter{},
			request.WithContext(mockContext{valueToReturnWhenValueMethodCalled: getMapWithDocIDThatCannotBeEscaped()}))

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.UnescapeFailure+messages.FailWriteResponse,
				docIDPathVariable, errFailingResponseWriter, errFailingResponseWriter))
	})
	t.Run("Response writer fails while writing read document error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		req, err := http.NewRequest(http.MethodGet, "", nil)
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)

		op.readDocumentHandler(failingResponseWriter{}, req)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.ReadDocumentFailure, testDocID, testVaultID, messages.ErrVaultNotFound))
	})
	t.Run("Response writer fails while writing retrieved document", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		createConfigStoreExpectSuccess(t, op)

		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		request := http.Request{}

		op.readDocumentHandler(failingResponseWriter{},
			request.WithContext(mockContext{valueToReturnWhenValueMethodCalled: getMapWithValidVaultIDAndDocID(vaultID)}))

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.ReadDocumentSuccess+messages.FailWriteResponse,
				testDocID, vaultID, errFailingResponseWriter))
	})
}

func Test_writeReadAllDocumentsSuccess(t *testing.T) {
	t.Run("Fail to marshal all documents", func(t *testing.T) {
		rr := httptest.NewRecorder()

		writeReadAllDocumentsSuccess(rr, []json.RawMessage{[]byte("NotValid")}, testVaultID)

		require.Equal(t, http.StatusInternalServerError, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.FailToMarshalAllDocuments, testVaultID, "json: error calling "+
			"MarshalJSON for type json.RawMessage: invalid character 'N' looking for beginning of value"),
			rr.Body.String())
	})
}

func TestUpdateDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		originalEncryptedDoc := `{"id":"` + testDocID + `","sequence":0,"indexed":` + testIndexedAttributeCollections1 +
			`,` + `"jwe":` + testJWE1 + `}`
		storeEncryptedDocumentExpectSuccess(t, op, testDocID, originalEncryptedDoc, vaultID)

		newEncryptedDoc := `{"id":"` + testDocID + `","sequence":0,"indexed":` + testIndexedAttributeCollections2 +
			`,` + `"jwe":` + testJWE1 + `}`
		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte(newEncryptedDoc)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)

		createDocumentEndpointHandler := getHandler(t, op, updateDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		getDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)
		getDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, newEncryptedDoc, rr.Body.String())
	})
	t.Run("Failure - error while unmarshalling incoming document", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte("notAnEncryptedDocument")))
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID
		urlVars[docIDPathVariable] = testDocID

		req = mux.SetURLVars(req, urlVars)
		createDocumentEndpointHandler := getHandler(t, op, updateDocumentEndpoint, http.MethodPost)

		createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "Received a request to update document "+testDocID+
			" in vault "+vaultID+", but the document is invalid:")
	})
	t.Run("Failure - IDs from path variable and incoming document don't match", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		updateDocumentExpectError(t, op, []byte(testEncryptedDocument), vaultID, testDocID2,
			fmt.Sprintf(messages.InvalidDocumentForDocUpdate, testDocID2, vaultID, messages.MismatchedDocIDs),
			http.StatusBadRequest)
	})
	t.Run("Failure - invalid incoming document", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		testInvalidDoc := models.EncryptedDocument{ID: testDocID, JWE: nil}

		testValidDocBytes, err := json.Marshal(testInvalidDoc)
		require.NoError(t, err)

		updateDocumentExpectError(t, op, testValidDocBytes, vaultID, testDocID,
			fmt.Sprintf(messages.InvalidDocumentForDocUpdate, testDocID, vaultID,
				fmt.Sprintf(messages.InvalidRawJWE, messages.BlankJWEAlg)), http.StatusBadRequest)
	})
	t.Run("Failure - vault not found", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		updateDocumentExpectError(t, op, []byte(testEncryptedDocument), testVaultID, testDocID,
			fmt.Sprintf(messages.UpdateDocumentFailure, testDocID, testVaultID, messages.ErrVaultNotFound),
			http.StatusNotFound)
	})
	t.Run("Failure - other error while opening store", func(t *testing.T) {
		op := New(&Config{Provider: &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 2,
			errOpenStore:                     errors.New("test error"),
		}})

		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		newEncryptedDoc := `{"id":"` + testDocID + `","sequence":0,"indexed":` + testIndexedAttributeCollections2 +
			`,` + `"jwe":` + testJWE1 + `}`

		updateDocumentExpectError(t, op, []byte(newEncryptedDoc), vaultID, testDocID,
			fmt.Sprintf(messages.UpdateDocumentFailure, testDocID, vaultID, "test error"), http.StatusBadRequest)
	})
	t.Run("Failure - document to be updated does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		newEncryptedDoc := `{"id":"` + testDocID + `","sequence":0,"indexed":` + testIndexedAttributeCollections2 +
			`,` + `"jwe":` + testJWE1 + `}`

		updateDocumentExpectError(t, op, []byte(newEncryptedDoc), vaultID, testDocID,
			fmt.Sprintf(messages.UpdateDocumentFailure, testDocID, vaultID, messages.ErrDocumentNotFound),
			http.StatusNotFound)
	})
	t.Run("Response writer fails while writing request read error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})

		op.updateDocumentHandler(failingResponseWriter{}, &http.Request{Body: failingReadCloser{}})

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents, errFailingReadCloser.Error())
		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents, errFailingResponseWriter.Error())
		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents, "Failed to read request body:")
	})
	t.Run("Response writer fails while writing vault not found error", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)

		op.updateDocument(&failingResponseWriter{}, []byte(testEncryptedDocument), testDocID, testVaultID)
		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents, "Failed to update document "+
			testDocID+" in vault "+testVaultID+": specified vault does not exist.")
		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents, errFailingResponseWriter.Error())
	})
}

func TestDeleteDocument(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = vaultID
		urlVars[docIDPathVariable] = testDocID

		req, err := http.NewRequest("DELETE", "", nil)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		req = mux.SetURLVars(req, urlVars)

		deleteDocumentEndpointHandler := getHandler(t, op, deleteDocumentEndpoint, http.MethodDelete)
		deleteDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		req, err = http.NewRequest("GET", "", nil)
		require.NoError(t, err)

		rr = httptest.NewRecorder()
		req = mux.SetURLVars(req, urlVars)

		getDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)
		getDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
		require.Equal(t, http.StatusNotFound, rr.Code)
		require.Equal(t, fmt.Sprintf(messages.ReadDocumentFailure, testDocID, vaultID, messages.ErrDocumentNotFound),
			rr.Body.String())
	})
	t.Run("Failure - unable to escape vault ID path variable", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		deleteDocumentExpectError(t, op, "%", testDocID, fmt.Sprintf(messages.UnescapeFailure,
			vaultIDPathVariable, `invalid URL escape "%"`), http.StatusBadRequest)
	})
	t.Run("Failure - unable to escape doc ID path variable", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		deleteDocumentExpectError(t, op, testVaultID, "%", fmt.Sprintf(messages.UnescapeFailure,
			docIDPathVariable, `invalid URL escape "%"`), http.StatusBadRequest)
	})
	t.Run("Failure - vault does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)

		deleteDocumentExpectError(t, op, testVaultID, testDocID, fmt.Sprintf(messages.DeleteDocumentFailure, testDocID,
			testVaultID, messages.ErrVaultNotFound), http.StatusNotFound)
	})
	t.Run("Failure - other error while opening store", func(t *testing.T) {
		op := New(&Config{Provider: &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 2,
			errOpenStore:                     errors.New("test error"),
		}})

		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		deleteDocumentExpectError(t, op, vaultID, testDocID, fmt.Sprintf(messages.DeleteDocumentFailure, testDocID,
			vaultID, "test error"), http.StatusBadRequest)
	})
	t.Run("Failure - document does not exist", func(t *testing.T) {
		op := New(&Config{Provider: memedvprovider.NewProvider()})
		createConfigStoreExpectSuccess(t, op)
		vaultID, _ := createDataVaultExpectSuccess(t, op)

		deleteDocumentExpectError(t, op, vaultID, testDocID, fmt.Sprintf(messages.DeleteDocumentFailure, testDocID,
			vaultID, messages.ErrDocumentNotFound), http.StatusNotFound)
	})
	t.Run("Response writer fails while writing delete document failure", func(t *testing.T) {
		writeDeleteDocumentFailure(failingResponseWriter{}, errors.New("test error"), testDocID, testVaultID)

		require.Contains(t, mockLoggerProvider.MockLogger.AllLogContents,
			fmt.Sprintf(messages.DeleteDocumentFailure+messages.FailWriteResponse, testDocID, testVaultID, "test error",
				errFailingResponseWriter))
	})
}

func TestBatch(t *testing.T) {
	upsertNewDoc1 := models.VaultOperation{
		Operation:         models.UpsertDocumentVaultOperation,
		EncryptedDocument: models.EncryptedDocument{ID: testDocID, JWE: []byte(testJWE1)},
	}

	upsertNewDoc2 := models.VaultOperation{
		Operation:         models.UpsertDocumentVaultOperation,
		EncryptedDocument: models.EncryptedDocument{ID: testDocID2, JWE: []byte(testJWE2)},
	}

	upsertExistingDoc1 := models.VaultOperation{
		Operation:         models.UpsertDocumentVaultOperation,
		EncryptedDocument: models.EncryptedDocument{ID: testDocID2, JWE: []byte(testJWE1)},
	}

	deleteExistingDoc1 := models.VaultOperation{
		Operation:  models.DeleteDocumentVaultOperation,
		DocumentID: testDocID,
	}

	invalidOperation := models.VaultOperation{
		Operation: "invalidOperationName",
	}

	upsertInvalidDoc := models.VaultOperation{
		Operation:         models.UpsertDocumentVaultOperation,
		EncryptedDocument: models.EncryptedDocument{},
	}

	deleteMissingDocumentID := models.VaultOperation{
		Operation: models.DeleteDocumentVaultOperation,
	}

	t.Run("Success: upsert (create), upsert (create), upsert (update)", func(t *testing.T) {
		rr, vaultID := doBatchCall(t, &models.Batch{upsertNewDoc1, upsertNewDoc2, upsertExistingDoc1},
			memedvprovider.NewProvider())

		require.Equal(t, `["/encrypted-data-vaults/`+vaultID+`/documents/`+testDocID+`"`+
			`,"/encrypted-data-vaults/`+vaultID+`/documents/`+testDocID2+
			`","/encrypted-data-vaults/`+vaultID+`/documents/`+testDocID2+`"]`,
			rr.Body.String())
		require.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("Success: upsert (create), upsert (create), delete", func(t *testing.T) {
		rr, vaultID := doBatchCall(t, &models.Batch{upsertNewDoc1, upsertNewDoc2, deleteExistingDoc1},
			memedvprovider.NewProvider())

		require.Equal(t, `["/encrypted-data-vaults/`+vaultID+`/documents/`+testDocID+`"`+
			`,"/encrypted-data-vaults/`+vaultID+`/documents/`+testDocID2+`",""]`,
			rr.Body.String())
		require.Equal(t, http.StatusOK, rr.Code)
	})
	t.Run("Failure: upsert (create), upsert (create), invalid operation", func(t *testing.T) {
		rr, _ := doBatchCall(t, &models.Batch{upsertNewDoc1, upsertNewDoc2, invalidOperation},
			memedvprovider.NewProvider())

		require.Equal(t, `["validated but not executed","validated but not executed",`+
			`"invalidOperationName is not a valid vault operation"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: upsert (create) with an invalid encrypted document", func(t *testing.T) {
		rr, _ := doBatchCall(t, &models.Batch{upsertInvalidDoc}, memedvprovider.NewProvider())

		require.Equal(t, `["document ID must be a base58-encoded value"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: unable to escape vault ID", func(t *testing.T) {
		op := New(&Config{
			Provider:          memedvprovider.NewProvider(),
			EnabledExtensions: &EnabledExtensions{Batch: true},
		})

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte("")))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = "%"

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		batchEndpointHandler := getHandler(t, op, batchEndpoint, http.MethodPost)
		batchEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t,
			fmt.Sprintf(messages.UnescapeFailure, vaultIDPathVariable, `invalid URL escape "%"`),
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: unable to marshal request", func(t *testing.T) {
		op := New(&Config{
			Provider:          memedvprovider.NewProvider(),
			EnabledExtensions: &EnabledExtensions{Batch: true},
		})

		req, err := http.NewRequest("POST", "", bytes.NewBuffer([]byte("Incorrect format")))
		require.NoError(t, err)

		urlVars := make(map[string]string)
		urlVars[vaultIDPathVariable] = testVaultID

		req = mux.SetURLVars(req, urlVars)

		rr := httptest.NewRecorder()

		batchEndpointHandler := getHandler(t, op, batchEndpoint, http.MethodPost)
		batchEndpointHandler.Handle().ServeHTTP(rr, req)

		require.Equal(t,
			fmt.Sprintf(messages.InvalidBatch, testVaultID,
				"invalid character 'I' looking for beginning of value"),
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: delete with a missing document ID", func(t *testing.T) {
		rr, _ := doBatchCall(t, &models.Batch{deleteMissingDocumentID}, memedvprovider.NewProvider())

		require.Equal(t, `["document ID cannot be empty for a delete operation"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: unable to create new document in underlying storage provider", func(t *testing.T) {
		errTestUpsertBulk := errors.New("upsert bulk error")
		rr, _ := doBatchCall(t, &models.Batch{upsertNewDoc1}, &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 3,
			errStoreUpsertBulk:               errTestUpsertBulk,
			errStoreGet:                      storage.ErrValueNotFound,
		})

		require.Equal(t, `["`+errTestUpsertBulk.Error()+`"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: unable to update document in underlying storage provider", func(t *testing.T) {
		errTestUpsertBulk := errors.New("upsert bulk error")
		rr, _ := doBatchCall(t, &models.Batch{upsertNewDoc1}, &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 4,
			errStoreUpsertBulk:               errTestUpsertBulk,
		})

		require.Equal(t, `["`+errTestUpsertBulk.Error()+`"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
	t.Run("Failure: unable to delete document in underlying storage provider", func(t *testing.T) {
		errTestDelete := errors.New("delete error")
		rr, _ := doBatchCall(t, &models.Batch{deleteExistingDoc1}, &mockEDVProvider{
			numTimesOpenStoreCalledBeforeErr: 3,
			errStoreDelete:                   errTestDelete,
		})

		require.Equal(t, `["`+errTestDelete.Error()+`"]`,
			rr.Body.String())
		require.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func doBatchCall(t *testing.T, batch *models.Batch,
	provider edvprovider.EDVProvider) (*httptest.ResponseRecorder, string) {
	op := New(&Config{
		Provider:          provider,
		EnabledExtensions: &EnabledExtensions{Batch: true},
	})

	createConfigStoreExpectSuccess(t, op)

	vaultID, _ := createDataVaultExpectSuccess(t, op)

	batchBytes, err := json.Marshal(batch)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", "", bytes.NewBuffer(batchBytes))
	require.NoError(t, err)

	urlVars := make(map[string]string)
	urlVars[vaultIDPathVariable] = vaultID

	req = mux.SetURLVars(req, urlVars)

	rr := httptest.NewRecorder()

	batchEndpointHandler := getHandler(t, op, batchEndpoint, http.MethodPost)
	batchEndpointHandler.Handle().ServeHTTP(rr, req)

	return rr, vaultID
}

func updateDocumentExpectError(t *testing.T, op *Operation, requestBody []byte, pathVarVaultID,
	pathVarDocID, expectedErrorString string, expectedErrorCode int) {
	req, err := http.NewRequest("POST", "", bytes.NewBuffer(requestBody))
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	urlVars := make(map[string]string)
	urlVars[vaultIDPathVariable] = pathVarVaultID
	urlVars[docIDPathVariable] = pathVarDocID

	req = mux.SetURLVars(req, urlVars)

	createDocumentEndpointHandler := getHandler(t, op, updateDocumentEndpoint, http.MethodPost)
	createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
	require.Equal(t, expectedErrorCode, rr.Code)
	require.Equal(t, expectedErrorString, rr.Body.String())
}

func deleteDocumentExpectError(t *testing.T, op *Operation, pathVarVaultID, pathVarDocID, expectedErrorString string,
	expectedErrorCode int) {
	urlVars := make(map[string]string)
	urlVars[vaultIDPathVariable] = pathVarVaultID
	urlVars[docIDPathVariable] = pathVarDocID

	req, err := http.NewRequest("DELETE", "", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	req = mux.SetURLVars(req, urlVars)

	deleteDocumentEndpointHandler := getHandler(t, op, deleteDocumentEndpoint, http.MethodDelete)
	deleteDocumentEndpointHandler.Handle().ServeHTTP(rr, req)
	require.Equal(t, expectedErrorCode, rr.Code)
	require.Equal(t, expectedErrorString, rr.Body.String())
}

func createConfigStoreExpectSuccess(t *testing.T, op *Operation) {
	err := op.vaultCollection.provider.CreateStore(dataVaultConfigurationStoreName)
	require.NoError(t, err)
}

func storeSampleConfigExpectSuccess(t *testing.T, op *Operation) {
	store, err := op.vaultCollection.provider.OpenStore(dataVaultConfigurationStoreName)
	require.NoError(t, err)

	err = store.StoreDataVaultConfiguration(&models.DataVaultConfiguration{ReferenceID: testReferenceID},
		testVaultID)
	require.NoError(t, err)
}

// returns created test vault ID
func createDataVaultExpectSuccess(t *testing.T, op *Operation) (string, []byte) {
	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer([]byte(testDataVaultConfiguration)))
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
	createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

	require.Equal(t, http.StatusCreated, rr.Code)
	require.Contains(t, rr.Header().Get("Location"), "/encrypted-data-vaults/")

	vaultID := getVaultIDFromURL(rr.Header().Get("Location"))

	return vaultID, rr.Body.Bytes()
}

func createDataVaultExpectError(t *testing.T, request *models.DataVaultConfiguration, expectedError string) {
	op := New(&Config{Provider: memedvprovider.NewProvider()})

	configBytes, err := json.Marshal(request)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "", bytes.NewBuffer(configBytes))
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	createVaultEndpointHandler := getHandler(t, op, createVaultEndpoint, http.MethodPost)
	createVaultEndpointHandler.Handle().ServeHTTP(rr, req)

	require.Equal(t, expectedError, rr.Body.String())
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func storeEncryptedDocumentExpectSuccess(t *testing.T, op *Operation, testDocID, encryptedDoc, vaultID string) {
	req, err := http.NewRequest("POST", "",
		bytes.NewBuffer([]byte(encryptedDoc)))
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	urlVars := make(map[string]string)
	urlVars[vaultIDPathVariable] = vaultID

	req = mux.SetURLVars(req, urlVars)

	createDocumentEndpointHandler := getHandler(t, op, createDocumentEndpoint, http.MethodPost)

	createDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

	require.Empty(t, rr.Body.String())
	require.Equal(t, http.StatusCreated, rr.Code)
	require.Equal(t, "/encrypted-data-vaults/"+vaultID+"/"+"documents/"+testDocID, rr.Header().Get("Location"))
}

func readDocumentExpectSuccess(t *testing.T, op *Operation) {
	vaultID, _ := createDataVaultExpectSuccess(t, op)

	storeEncryptedDocumentExpectSuccess(t, op, testDocID, testEncryptedDocument, vaultID)

	readDocumentEndpointHandler := getHandler(t, op, readDocumentEndpoint, http.MethodGet)

	req, err := http.NewRequest(http.MethodGet, "", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()

	urlVars := make(map[string]string)
	urlVars[vaultIDPathVariable] = vaultID
	urlVars[docIDPathVariable] = testDocID

	req = mux.SetURLVars(req, urlVars)

	readDocumentEndpointHandler.Handle().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)

	require.Equal(t, testEncryptedDocument, rr.Body.String())
}

func getHandler(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	return getHandlerWithError(t, op, pathToLookup, methodToLookup)
}

func getHandlerWithError(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	return handlerLookup(t, op, pathToLookup, methodToLookup)
}

func handlerLookup(t *testing.T, op *Operation, pathToLookup, methodToLookup string) Handler {
	handlers := op.GetRESTHandlers()
	require.NotEmpty(t, handlers)

	for _, h := range handlers {
		if h.Path() == pathToLookup && h.Method() == methodToLookup {
			return h
		}
	}

	require.Fail(t, "unable to find handler")

	return nil
}

// Extract and return vaultID from vaultLocationURL: /encrypted-data-vaults/{vaultID}
func getVaultIDFromURL(vaultLocationURL string) string {
	vaultLocationSplitUp := strings.Split(vaultLocationURL, "/")

	return vaultLocationSplitUp[len(vaultLocationSplitUp)-1]
}

func getDataVaultConfig(controller, kekID, kekType, hmacID, hmacType string,
	delegator, invoker []string) *models.DataVaultConfiguration {
	config := &models.DataVaultConfiguration{
		Sequence:    0,
		Controller:  controller,
		Invoker:     invoker,
		Delegator:   delegator,
		ReferenceID: testReferenceID,
		KEK: models.IDTypePair{
			ID:   kekID,
			Type: kekType,
		},
		HMAC: models.IDTypePair{
			ID:   hmacID,
			Type: hmacType,
		},
	}

	return config
}

func getMapWithValidVaultIDAndDocID(testVaultID string) map[string]string {
	return map[string]string{
		"vaultID": testVaultID,
		"docID":   testDocID,
	}
}

func getMapWithVaultIDThatCannotBeEscaped() map[string]string {
	return map[string]string{
		"vaultID": "%",
	}
}

func getMapWithDocIDThatCannotBeEscaped() map[string]string {
	return map[string]string{
		"docID": "%",
	}
}

type mockAuthService struct {
	createValue []byte
	createErr   error
}

func (m *mockAuthService) Create(resourceID, verificationMethod string) ([]byte, error) {
	return m.createValue, m.createErr
}
