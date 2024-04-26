package blockfrost

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/polygon-edge/e2e-polybft/cardanofw"
	"github.com/0xPolygon/polygon-edge/helper/common"
)

type BlockFrost struct {
	Id          int
	RootDir     string
	ClusterName string
}

type PostgresConfig struct {
	User     string
	Password string
	Db       string
}

func NewBlockFrost(cluster *cardanofw.TestCardanoCluster, id int) (*BlockFrost, error) {
	clusterName := fmt.Sprintf("cluster-%d-blockfrost", id)
	dockerDir := path.Join("../../e2e-docker-tmp", clusterName)
	err := os.RemoveAll(dockerDir)
	if err != nil {
		return nil, err
	}
	if err := common.CreateDirSafe(dockerDir, 0750); err != nil {
		return nil, err
	}

	err = resolvePostgresFiles(dockerDir)
	if err != nil {
		return nil, err
	}

	err = resolveGenesisFiles(cluster.Config.TmpDir, dockerDir)
	if err != nil {
		return nil, err
	}

	postgresPort := 6000 + id
	blockfrostPort := 12000 + id

	err = resolveConfigFiles(cluster.Config.TmpDir, dockerDir)
	if err != nil {
		return nil, err
	}

	err = resolveDockerCompose(dockerDir, postgresPort, blockfrostPort, cluster.Config.Id)
	if err != nil {
		return nil, err
	}

	return &BlockFrost{
		Id:          id,
		RootDir:     dockerDir,
		ClusterName: clusterName,
	}, nil
}

func (bf *BlockFrost) Start() error {
	dockerFile := filepath.Join(bf.RootDir, "docker-compose.yml")

	runCommand("docker-compose", []string{"-f", dockerFile, "up", "-d"})

	return nil
}

func (bf *BlockFrost) Stop() error {
	dockerFile := filepath.Join(bf.RootDir, "docker-compose.yml")

	runCommand("docker-compose", []string{"-f", dockerFile, "down"})

	// remove volumes
	runCommand("docker", []string{"volume", "rm",
		fmt.Sprintf("%s_db-sync-data", bf.ClusterName),
		fmt.Sprintf("%s_postgres", bf.ClusterName)})

	return nil
}

func resolvePostgresFiles(dockerDir string) error {
	secretsPath := path.Join(dockerDir, "secrets")
	if err := common.CreateDirSafe(secretsPath, 0750); err != nil {
		return err
	}

	postgresConfig := getPostgresConfig()

	dbFile := filepath.Join(secretsPath, "postgres_db")
	if err := os.WriteFile(dbFile, []byte(postgresConfig.Db), 0644); err != nil {
		return err
	}

	pwFile := filepath.Join(secretsPath, "postgres_password")
	if err := os.WriteFile(pwFile, []byte(postgresConfig.Password), 0644); err != nil {
		return err
	}

	userFile := filepath.Join(secretsPath, "postgres_user")
	if err := os.WriteFile(userFile, []byte(postgresConfig.User), 0644); err != nil {
		return err
	}

	return nil
}

func resolveGenesisFiles(rootDir string, dockerDir string) error {
	nodeGenesis := path.Join(rootDir, "genesis")

	dockerGenesis := path.Join(dockerDir, "genesis")
	if err := common.CreateDirSafe(dockerGenesis, 0750); err != nil {
		return err
	}

	copyDirectory(nodeGenesis, dockerGenesis)

	return nil
}

func resolveConfigFiles(rootDir string, dockerDir string) error {
	configPath := path.Join(dockerDir, "config")
	if err := common.CreateDirSafe(configPath, 0750); err != nil {
		return err
	}

	// DBSync config
	dbsyncPath := path.Join(configPath, "dbsync")
	if err := common.CreateDirSafe(dbsyncPath, 0750); err != nil {
		return err
	}

	dbsyncConfigSrc := "../blockfrost/docker-files/dbsync_config.json"
	dbsyncConfig := filepath.Join(dbsyncPath, "config.json")
	copyFile(dbsyncConfigSrc, dbsyncConfig)

	nodeConfigSrc := "../blockfrost/docker-files/node_config.yaml"
	nodeConfig := filepath.Join(dbsyncPath, "config.yaml")
	copyFile(nodeConfigSrc, nodeConfig)
	// replaceLine(nodeConfig, "hasEKG: 12788", fmt.Sprintf("hasEKG: %d", ekgPort))

	byronGenesis := filepath.Join(rootDir, "genesis/byron/genesis.json")
	byronHash, err := runCommand("cardano-cli", []string{"byron", "genesis", "print-genesis-hash", "--genesis-json", byronGenesis})
	if err != nil {
		return err
	}
	appendToFile(nodeConfig, fmt.Sprintf("ByronGenesisHash: %s", byronHash))

	shelleyGenesis := filepath.Join(rootDir, "genesis/shelley/genesis.json")
	shelleyHash, err := runCommand("cardano-cli", []string{"shelley", "genesis", "hash", "--genesis", shelleyGenesis})
	if err != nil {
		return err
	}
	appendToFile(nodeConfig, fmt.Sprintf("ShelleyGenesisHash: %s", shelleyHash))

	alonzoGenesis := filepath.Join(rootDir, "genesis/shelley/genesis.alonzo.json")
	alonzoHash, err := runCommand("cardano-cli", []string{"alonzo", "genesis", "hash", "--genesis", alonzoGenesis})
	if err != nil {
		return err
	}
	appendToFile(nodeConfig, fmt.Sprintf("AlonzoGenesisHash: %s", alonzoHash))

	conwayGenesis := filepath.Join(rootDir, "genesis/shelley/genesis.conway.json")
	conwayHash, err := runCommand("cardano-cli", []string{"conway", "genesis", "hash", "--genesis", conwayGenesis})
	if err != nil {
		return err
	}
	appendToFile(nodeConfig, fmt.Sprintf("ConwayGenesisHash: %s", conwayHash))

	return nil
}

func resolveDockerCompose(dockerDir string, postgresPort int, blockfrostPort int, clusterId int) error {
	dockerFileSrc := "../blockfrost/docker-files/docker-compose.yml"
	dockerFile := filepath.Join(dockerDir, "docker-compose.yml")
	copyFile(dockerFileSrc, dockerFile)

	replaceLine(dockerFile, "      - ../../e2e-docker-tmp/cluster-1:/node-data", fmt.Sprintf("      - ../../e2e-docker-tmp/cluster-%d:/node-data", clusterId))

	replaceLine(dockerFile, "      - ${POSTGRES_PORT:-5432}:5432", fmt.Sprintf("      - ${POSTGRES_PORT:-%d}:5432", postgresPort))
	// replaceLine(dockerFile, "      - POSTGRES_PORT=5432", fmt.Sprintf("      - POSTGRES_PORT=%d", postgresPort))

	replaceLine(dockerFile, "      - ${POSTGRES_PORT:-3000}:3000", fmt.Sprintf("      - ${POSTGRES_PORT:-%d}:%d", blockfrostPort, blockfrostPort))
	replaceLine(dockerFile, "      - BLOCKFROST_CONFIG_SERVER_PORT=3000", fmt.Sprintf("      - BLOCKFROST_CONFIG_SERVER_PORT=%d", blockfrostPort))

	return nil
}

func GetTopology(topologyFile string) (string, error) {
	port, err := GetFirstPortFromTopologyFile(topologyFile)
	if err != nil {
		return "", err
	}

	topologyBase := `
{
	"Producers": [
		{
			"addr": "cluster-1-node-1",
			"port": %s,
			"valency": 1
		}
	]
}`

	topology := fmt.Sprintf(topologyBase, port)
	return topology, nil
}

func getPostgresConfig() *PostgresConfig {
	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		// fallback
		user = "postgres"
	}

	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		// fallback
		password = "password"
	}

	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		// fallback
		dbName = "testdb"
	}

	return &PostgresConfig{
		User:     user,
		Password: password,
		Db:       dbName,
	}
}

// It was used for relay node, might be useful again in the future
func GetFirstPortFromTopologyFile(topologyFile string) (string, error) {
	file, err := os.Open(topologyFile)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return "", nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, `"port"`) {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				port := strings.TrimSpace(strings.Trim(parts[1], ","))
				return port, nil
			}
		}
	}

	err = scanner.Err()
	return "", err
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}

func copyDirectory(srcDir, dstDir string) error {
	files, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		srcFile := filepath.Join(srcDir, file.Name())
		dstFile := filepath.Join(dstDir, file.Name())

		if file.IsDir() {
			err = os.MkdirAll(dstFile, os.ModePerm)
			if err != nil {
				return err
			}
			err = copyDirectory(srcFile, dstFile)
			if err != nil {
				return err
			}
		} else {
			err = copyFile(srcFile, dstFile)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func appendToFile(filePath string, line string) {
	// Open file in append mode
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	// Create a writer
	writer := bufio.NewWriter(file)

	// Write the line to the file
	_, err = writer.WriteString(line)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return
	}

	// Flush the buffer to ensure the line is written to the file
	err = writer.Flush()
	if err != nil {
		fmt.Println("Error flushing writer:", err)
		return
	}
}

func replaceLine(filePath string, search string, replace string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	tempFile, err := os.CreateTemp("", "tempFile")
	if err != nil {
		return err
	}
	defer tempFile.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, search) {
			line = strings.Replace(line, search, replace, 1)
		}
		tempFile.WriteString(line + "\n")
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if err := os.Rename(tempFile.Name(), filePath); err != nil {
		return err
	}

	return nil
}

func runCommand(binary string, args []string, envVariables ...string) (string, error) {
	var (
		stdErrBuffer bytes.Buffer
		stdOutBuffer bytes.Buffer
	)

	cmd := exec.Command(binary, args...)
	cmd.Stderr = &stdErrBuffer
	cmd.Stdout = &stdOutBuffer
	cmd.Env = append(os.Environ(), envVariables...)

	err := cmd.Run()
	if stdErrBuffer.Len() > 0 {
		return "", errors.New(stdErrBuffer.String())
	} else if err != nil {
		return "", err
	}

	return stdOutBuffer.String(), nil
}

func ResetDbSync(startAfter int) error {
	time.Sleep(time.Duration(startAfter) * time.Second)
	found := false
	var wg sync.WaitGroup

	for !found {
		time.Sleep(2 * time.Second)

		args := []string{"ps", "--format", "{{.Names}}", "--filter", "name=db-sync"}
		res, _ := runCommand("docker", args)
		containers := strings.Split(res, "\n")

		if len(containers) == 0 {
			continue
		}

		found = true

		for _, cName := range containers {
			if cName == "" {
				continue
			}

			wg.Add(1)
			containerName := cName

			go func() {
				args := []string{"logs", containerName}

				for i := 0; i < 6; i++ {
					res, _ := runCommand("docker", args)
					logs := strings.Split(res, "\n")
					lastLog := logs[len(logs)-2] // last is empty string so we take one before last

					if strings.Contains(lastLog, "Creating Indexes. This may take a while.") {
						args := []string{"restart", containerName}
						runCommand("docker", args)
						i = 0
						continue
					}

					time.Sleep(20 * time.Second)
				}

				wg.Done()
				_ = args
			}()
		}
	}

	wg.Wait()

	return nil
}
