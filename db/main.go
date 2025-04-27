package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/FoolVPN-ID/megalodon/common/helper"
	logger "github.com/FoolVPN-ID/megalodon/log"
	"github.com/FoolVPN-ID/megalodon/sandbox"
	"github.com/FoolVPN-ID/megalodon/telegram/bot"
	"github.com/sagernet/sing/common/json"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

var safePattern = regexp.MustCompile("[^A-Za-z0-9-]")

type databaseStruct struct {
	client          *sql.DB
	dbUrl           string
	dbToken         string
	logger          *logger.LoggerStruct
	rawAccountTotal int
	uniqueIds       []string
	queries         []string
	ErrorValues     []string
}

func MakeDatabase() *databaseStruct {
	dbInstance := databaseStruct{
		dbUrl:   os.Getenv("TURSO_DATABASE_URL"),
		dbToken: os.Getenv("TURSO_AUTH_TOKEN"),
		logger:  logger.MakeLogger(),
	}

	dbInstance.connect()

	return &dbInstance
}

func (db *databaseStruct) connect() {
	client, err := sql.Open("libsql", fmt.Sprintf("%s?authToken=%s", db.dbUrl, db.dbToken))
	if err != nil {
		panic(err.Error())
	}

	db.logger.Success("[db] Client initialized")
	db.client = client
}

func (db *databaseStruct) Ping() {
	db.logger.Info("[db] Pinging...")
	if err := db.client.Ping(); err != nil {
		db.logger.Error("[db] Closed, initializing new client...")
		db.connect()
	} else {
		db.logger.Success("[db] Alive")
	}
}

func (db *databaseStruct) SyncAndClose() {
	db.logger.Info("[db] Closing client")
	if err := db.client.Close(); err != nil {
		db.logger.Error(err.Error())
	}
}

func (db *databaseStruct) createTableSafe() {
	var (
		crateTableQuery = `CREATE TABLE IF NOT EXISTS proxies (
			id INTEGER PRIMARY KEY,
			server STRING,
			ip STRING,
			server_port INT8,
			uuid STRING,
			password STRING,
			security STRING,
			alter_id INT2,
			method STRING,
			plugin STRING,
			plugin_opts STRING,
			host STRING,
			tls INT2,
			transport STRING,
			path STRING,
			service_name STRING,
			insecure INT2,
			sni STRING,
			remark STRING,
			conn_mode STRING,
			country_code STRING,
			region STRING,
			org STRING,
			vpn STRING,
			raw STRING
		);`
	)

	if _, err := db.client.Query(crateTableQuery); err == nil {
		db.logger.Info("[db] Table created")
	} else {
		db.logger.Error(err.Error())
		os.Exit(1)
	}
}

func (db *databaseStruct) Save(results []sandbox.TestResultStruct) error {
	db.createTableSafe()
	db.queries = append(db.queries, "DELETE FROM proxies;")
	db.queries = append(db.queries, db.buildInsertQuery(results)...)

	var (
		err error
		tgb = bot.MakeTGgBot()
	)

	defer func() {
		if err != nil {
			tgb.SendTextFileToAdmin(fmt.Sprintf("error_query_%v.txt", time.Now().Unix()), err.Error(), "Error Query")
		}
	}()

	tgb.SendTextFileToAdmin(fmt.Sprintf("query_%v.txt", time.Now().Unix()), strings.Join(db.queries, "\n"), "DB Query")
	if len(db.ErrorValues) > 0 {
		tgb.SendTextFileToAdmin(fmt.Sprintf("error_%v.txt", time.Now().Unix()), strings.Join(db.ErrorValues, "\n"), "Error Values")
	}

	// Begin transaction
	txCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	transaction, err := db.client.BeginTx(txCtx, nil)
	if err != nil {
		db.logger.Error(err.Error())
		return err
	}

	for _, dbQuery := range db.queries {
		if _, err := transaction.Exec(dbQuery); err != nil {
			transaction.Rollback()
			db.logger.Error(err.Error())
			return err
		}
	}

	if err := transaction.Commit(); err != nil {
		transaction.Rollback()
		db.logger.Error(err.Error())
		return err
	} else {
		db.logger.Info("=========================")
		db.logger.Success("[db] Insert operation succeed")
		db.logger.Info(fmt.Sprintf("Total raw account: %d", db.rawAccountTotal))
		db.logger.Info(fmt.Sprintf("Total account saved: %d", len(db.uniqueIds)))

		// Report
		tgb.SendTextToAdmin(fmt.Sprintf("Account saved: %d", len(db.uniqueIds)))
	}

	return nil
}

func (db *databaseStruct) buildInsertQuery(results []sandbox.TestResultStruct) []string {
	db.rawAccountTotal = len(results)

	tableFieldValues := []ProxyFieldStruct{}
	for _, result := range results {
		var (
			fieldValues = ProxyFieldStruct{}
			outbound    = result.Outbound
		)

		var (
			outboundMapping = map[string]any{}
			outboundByte, _ = json.Marshal(outbound.Options)
		)
		json.Unmarshal(outboundByte, &outboundMapping)

		// Geoip
		fieldValues.Ip = result.ConfigGeoip.IP
		fieldValues.CountryCode = result.ConfigGeoip.Country
		fieldValues.Region = helper.GetRegionFromCC(fieldValues.CountryCode)
		fieldValues.Org = result.ConfigGeoip.AsOrganization

		// Common
		fieldValues.VPN = outbound.Type
		fieldValues.Server = outboundMapping["server"].(string)
		fieldValues.ServerPort = int(outboundMapping["server_port"].(float64))
		fieldValues.Transport = "tcp"
		fieldValues.Raw = result.RawConfig

		// Here we go assertion hell
		if uuid, ok := outboundMapping["uuid"].(string); ok {
			fieldValues.UUID = safePattern.ReplaceAllString(uuid, "")
		}
		if password, ok := outboundMapping["password"].(string); ok {
			fieldValues.Password = safePattern.ReplaceAllString(password, "")
		}
		if security, ok := outboundMapping["security"].(string); ok {
			fieldValues.Security = security
		}
		if alterId, ok := outboundMapping["alter_id"].(int); ok {
			fieldValues.AlterId = alterId
		}
		if method, ok := outboundMapping["method"].(string); ok {
			fieldValues.Method = method
		}
		if plugin, ok := outboundMapping["plugin"].(string); ok {
			fieldValues.Plugin = plugin
		}
		if pluginOpts, ok := outboundMapping["plugin_opts"].(string); ok {
			fieldValues.PluginOpts = pluginOpts
		}

		// Transport
		if outboundMapping["transport"] != nil {
			transportMapping := outboundMapping["transport"].(map[string]any)
			if transportType, ok := transportMapping["type"].(string); ok {
				fieldValues.Transport = transportType
			}
			if serviceName, ok := transportMapping["service_name"].(string); ok {
				fieldValues.ServiceName = serviceName
			}
			if path, ok := transportMapping["path"].(string); ok {
				fieldValues.Path = path
			}
			if host, ok := transportMapping["host"].(string); ok {
				fieldValues.Host = host
			}
			if transportMapping["headers"] != nil {
				headersMapping := transportMapping["headers"].(map[string]any)
				if host, ok := headersMapping["Host"].(string); ok {
					fieldValues.Host = host
				}
			}
		}

		// TLS
		tlsStr := "NTLS"
		if outboundMapping["tls"] != nil {
			tlsMapping := outboundMapping["tls"].(map[string]any)
			if enabled, ok := tlsMapping["enabled"].(bool); ok {
				fieldValues.TLS = enabled
				if enabled {
					tlsStr = "TLS"
				}
			}
			if insecure, ok := tlsMapping["insecure"].(bool); ok {
				fieldValues.Insecure = insecure
			}
			if sni, ok := tlsMapping["server_name"].(string); ok {
				fieldValues.SNI = sni
			}
		} else if outboundMapping["plugin_opts"] != nil {
			if strings.Contains(outboundMapping["plugin_opts"].(string), "tls") {
				fieldValues.TLS = true
				tlsStr = "TLS"
			}
		} else if fieldValues.ServerPort == 443 || fieldValues.ServerPort == 8443 {
			fieldValues.TLS = true
			tlsStr = "TLS"
		}

		for _, connMode := range result.TestPassed {
			fieldValues.ConnMode = connMode
			fieldValues.Remark = strings.ToUpper(fmt.Sprintf("%d %s %s %s %s %s", len(tableFieldValues)+1, helper.CCToEmoji(fieldValues.CountryCode), fieldValues.Org, fieldValues.Transport, connMode, tlsStr))

			// Check if same account exists
			if !db.checkIsExists(fieldValues) {
				tableFieldValues = append(tableFieldValues, fieldValues)
			}
		}
	}
	// Manual memori clean up, due large size variable
	results = nil
	runtime.GC()

	// Build queries
	values := []string{}
	for _, fieldValue := range tableFieldValues {
		value := "("

		value += fmt.Sprintf("'%s', ", fieldValue.Server)
		value += fmt.Sprintf("'%s', ", fieldValue.Ip)
		value += fmt.Sprintf("%d, ", fieldValue.ServerPort)
		value += fmt.Sprintf("'%s', ", fieldValue.UUID)
		value += fmt.Sprintf("'%s', ", fieldValue.Password)
		value += fmt.Sprintf("'%s', ", fieldValue.Security)
		value += fmt.Sprintf("%d, ", fieldValue.AlterId)
		value += fmt.Sprintf("'%v', ", fieldValue.Method)
		value += fmt.Sprintf("'%v', ", fieldValue.Plugin)
		value += fmt.Sprintf("'%v', ", fieldValue.PluginOpts)
		value += fmt.Sprintf("'%v', ", fieldValue.Host)
		value += fmt.Sprintf("%t, ", fieldValue.TLS)
		value += fmt.Sprintf("'%s', ", fieldValue.Transport)
		value += fmt.Sprintf("'%s', ", fieldValue.Path)
		value += fmt.Sprintf("'%s', ", fieldValue.ServiceName)
		value += fmt.Sprintf("%t, ", fieldValue.Insecure)
		value += fmt.Sprintf("'%s', ", fieldValue.SNI)
		value += fmt.Sprintf("'%s', ", fieldValue.Remark)
		value += fmt.Sprintf("'%s', ", fieldValue.ConnMode)
		value += fmt.Sprintf("'%s', ", fieldValue.CountryCode)
		value += fmt.Sprintf("'%s', ", fieldValue.Region)
		value += fmt.Sprintf("'%s', ", fieldValue.Org)
		value += fmt.Sprintf("'%s', ", fieldValue.VPN)
		value += fmt.Sprintf("'%s'", fieldValue.Raw)

		value += ")"

		values = append(values, value)
	}

	baseInsertQuery := `INSERT INTO proxies (
		SERVER,
		IP,
		SERVER_PORT,
		UUID, PASSWORD,
		SECURITY,
		ALTER_ID,
		METHOD,
		PLUGIN,
		PLUGIN_OPTS,
		HOST,
		TLS,
		TRANSPORT,
		PATH,
		SERVICE_NAME,
		INSECURE,
		SNI,
		REMARK,
		CONN_MODE,
		COUNTRY_CODE,
		REGION,
		ORG,
		VPN,
		RAW
	) VALUES`

	// Filter bad and build insert queries
	var (
		insertQueries = []string{}
		wg            sync.WaitGroup
		queue         = make(chan struct{}, 100)
		valueLength   = 100
		isValidating  = false
	)

	if isValidating {
		for i, value := range values {
			wg.Add(1)
			queue <- struct{}{}

			db.logger.Info(fmt.Sprintf("[%d/%d] Validating account format...", i, len(values)))
			go func(insertValue string) {
				defer func() {
					wg.Done()
					<-queue
				}()

				if err := db.validateQuery(fmt.Sprintf("%s %s", baseInsertQuery, value)); err != nil {
					db.logger.Error(err.Error())
					values[i] = ""
				} else {
					values[i] = insertValue
				}
			}(value)
		}
		wg.Wait()
	}

	for i := 0; i < len(values); i += valueLength {
		end := i + valueLength
		if end > len(values) {
			end = len(values)
		}
		insertQueries = append(insertQueries, fmt.Sprintf(`%s %s;`, baseInsertQuery, strings.Join(helper.RemoveEmptyStringFromList(values[i:end]), ",")))
	}

	return insertQueries
}

func (db *databaseStruct) validateQuery(query string) error {
	_, result := db.client.Exec(fmt.Sprintf("EXPLAIN %s;", query))
	return result
}

func (db *databaseStruct) makeUniqueId(field ProxyFieldStruct) string {
	// Server Port, UUID, Password, Plugin Opts, Path, Transport, Conn Mode, Country, Org, VPN
	uid := fmt.Sprintf("%d_%s_%s_%s_%s_%s_%s_%s_%s_%s", field.ServerPort, field.UUID, field.Password, field.PluginOpts, field.Path, field.Transport, field.ConnMode, field.CountryCode, field.Org, field.VPN)

	return uid
}

func (db *databaseStruct) checkIsExists(field ProxyFieldStruct) bool {
	uid := db.makeUniqueId(field)
	for _, existedUid := range db.uniqueIds {
		if existedUid == uid {
			return true
		}
	}

	db.uniqueIds = append(db.uniqueIds, uid)
	return false
}
