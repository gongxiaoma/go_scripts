/**
* Author: gongxiaoma
* Date：2024-10-22
 */
package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	alidns "github.com/alibabacloud-go/alidns-20150109/v2/client"
	aliopenapi "github.com/alibabacloud-go/darabonba-openapi/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/spf13/viper"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	dnspod "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/dnspod/v20210323"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// 定义变量或初始化
var (
	aliyunDomainSlice  []string
	tencentDomainSlice []string
	recordSlice        []string
	errlogFile         *os.File
	errlogger          *log.Logger
	infologFile        *os.File
	infologger         *log.Logger
	setpStatusMap      map[string][]string
	httpsDomainSum     = 0
	successText        = "执行完成"
	failText           = "执行失败"
	successColor       = "green"
	failColor          = "red"
)

// 定义调用通知接口的入参结构体
type MarkdownMessage struct {
	MsgType  string `json:"msgtype"`
	Markdown struct {
		Content string `json:"content"`
	} `json:"markdown"`
}

/**
* init函数，初始化信息
 */
func init() {
	// 打开或创建日志文件
	var _err error

	// 创建error日志文件
	errlogFile, _err = os.OpenFile("error.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if _err != nil {
		// 如果打开文件异常，使用标准错误输出记录错误
		log.Printf("打开或者创建error.log文件异常: %v", _err)

		// 初始化logger为标准输出，以防日志文件无法创建
		errlogger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		// 如果文件成功打开，初始化logger
		errlogger = log.New(errlogFile, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	}

	// 创建info日志文件
	infologFile, _err = os.OpenFile("info.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if _err != nil {
		// 如果打开文件失败，使用标准错误输出记录错误
		log.Printf("打开或者创建info.log文件异常: %v", _err)

		// 初始化logger为标准输出，以防日志文件无法创建
		infologger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		// 如果文件成功打开，初始化logger
		infologger = log.New(infologFile, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	}

	// 初始化setpStatusMap并设置默认值，只要后续流程没有重写这些值，那么相应阶段就是执行失败
	setpStatusMap = make(map[string][]string)
	setpStatusMap["aliyunInitStatus"] = []string{"执行失败", "red"}
	setpStatusMap["aliyunDescribeDomainsStatus"] = []string{"执行失败", "red"}
	setpStatusMap["aliyunDescribeDomainRecordsStatus"] = []string{"执行失败", "red"}
	setpStatusMap["tencentInitStatus"] = []string{"执行失败", "red"}
	setpStatusMap["tencentDescribeDomainsStatus"] = []string{"执行失败", "red"}
	setpStatusMap["tencentDescribeDomainRecordsStatus"] = []string{"执行失败", "red"}
	setpStatusMap["expirationHttpsDomainStatus"] = []string{"执行失败", "red"}
	setpStatusMap["reloadPrometheusStatus"] = []string{"执行失败", "red"}
	setpStatusMap["aliyunInitStatus"] = []string{"执行失败", "red"}
}

/**
* 程序退出之前操作
 */
func preClose() {
	// 关闭error日志文件
	if errlogFile != nil {
		errlogFile.Close()
	}

	// 关闭info日志文件
	if infologFile != nil {
		infologFile.Close()
	}
}

/**
* 初始化配置文件
 * @return error
*/
func GetConfig() (_err error) {
	viper.SetConfigName("config")
	viper.AddConfigPath("./config")
	viper.SetConfigType("yml")

	// 读取配置文件, 如果出错则退出
	if err := viper.ReadInConfig(); err != nil {
		fmt.Printf("Error reading config file, %s\n", err)
		return err
	}

	// 获取配置值
	//aliyun_key := viper.GetString("cloud.alibaba.aliyun_key") // 读取字符串
	return nil
}

/**
* 初始化阿里云SDK
 * @param accessKeyId
 * @param accessKeySecret
 * @return *alidns.Client
 * @return error
*/
func AliyunInit(accessKeyId *string, accessKeySecret *string, regionId *string) (alidnsClient *alidns.Client, _err error) {

	// &号表示创建了一个aliopenapi.Config类型的零值实例，并获取了这个实例的内存地址。这个地址被赋值给了config变量。因此config是一个指向aliopenapi.Config类型值的指针。
	config := &aliopenapi.Config{}
	config.AccessKeyId = accessKeyId
	config.AccessKeySecret = accessKeySecret
	config.RegionId = regionId

	// _result是一个指向alidns.Client类型的指针
	ailClient, _err := alidns.NewClient(config)
	return ailClient, _err
}

/**
* 初始化腾讯云SDK
 * @param secretId
 * @param secretKey
 * @return *dnspod.Client
 * @return error
*/
func TencentInit(secretId string, secretKey string) (txdnsClient *dnspod.Client, err error) {

	credential := common.NewCredential(
		secretId,
		secretKey,
	)

	// 实例化一个client选项，可选的，没有特殊需求可以跳过
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "dnspod.tencentcloudapi.com"

	// 实例化要请求产品的client对象,clientProfile是可选的
	txClient, err := dnspod.NewClient(credential, "", cpf)
	return txClient, err
}

/**
* 初始化阿里云和腾讯云SDK
 * @return *alidns.Client
 * @return *dnspod.Client
 * @return error
*/
func ClientInit() (aliyunClient *alidns.Client, tencentClient *dnspod.Client, _err error) {

	// 阿里云密钥
	aliyunAccessKeyId := viper.GetString("cloud.alibaba.aliyun_key")
	aliyunAccessKeySecret := viper.GetString("cloud.alibaba.aliyun_secret")
	regionId := viper.GetString("cloud.alibaba.region")

	// 腾讯云密钥
	tencentSecretId := viper.GetString("cloud.tencent.tencent_key")
	tencentSecretKey := viper.GetString("cloud.tencent.tencent_secret")

	// 初始阿里云化客户端
	ailClient, _err := AliyunInit(&aliyunAccessKeyId, &aliyunAccessKeySecret, &regionId)
	if _err != nil {
		setpStatusMap["aliyunInitStatus"] = []string{failText, failColor}
		errlogger.Printf("初始化阿里云SDK:执行失败: %v", _err)
		return nil, nil, _err
	} else {
		setpStatusMap["aliyunInitStatus"] = []string{successText, successColor}
		infologger.Printf("初始化阿里云SDK:执行完成")
	}

	// 初始腾讯云客户端
	txClient, _err := TencentInit(tencentSecretId, tencentSecretKey)
	if _err != nil {
		setpStatusMap["tencentInitStatus"] = []string{failText, failColor}
		errlogger.Printf("初始化腾讯云SDK:执行失败: %v", _err)
		return nil, nil, _err
	} else {
		setpStatusMap["tencentInitStatus"] = []string{successText, successColor}
		infologger.Printf("初始化腾讯云SDK:执行完成")
	}

	return ailClient, txClient, _err
}

/**
* 查询阿里云域名列表
 * @param *alidns.Client
 * @return error
*/
func AliyunDescribeDomains(client *alidns.Client) (_err error) {
	// 定义初始页码和每页大小
	pageNumber := 1
	pageSize := 20

	// for循环主要是循环每页
	for {
		// 创建一个指向dns.DescribeDomainsRequest类型结构体的指针，并初始化其成员变量PageNumber和PageSize
		// &alidns.DescribeDomainsRequest{}这部分创建了一个alidns.DescribeDomainsRequest类型的新实例，并且由于前面加了&，所以创建的是指向该实例的一个指针
		req := &alidns.DescribeDomainsRequest{
			PageNumber: tea.Int64(int64(pageNumber)),
			PageSize:   tea.Int64(int64(pageSize)),
		}

		resp, _err := client.DescribeDomains(req)
		if _err != nil {
			return _err
		}

		if len(resp.Body.Domains.Domain) == 0 {
			return nil
		} else {
			for _, domain := range resp.Body.Domains.Domain {
				aliyunDomainSlice = append(aliyunDomainSlice, *domain.DomainName)
			}
			// 更新页码以获取下一页
			pageNumber++
		}
	}
	return nil
}

/**
* 查询腾讯云域名列表
 * @param *dnspod.Client
 * @return error
*/
func TencentDescribeDomains(client *dnspod.Client) (_err error) {

	// 不用分页，默认显示3000条
	// 实例化一个请求对象,每个接口都会对应一个request对象
	request := dnspod.NewDescribeDomainListRequest()

	// 返回的resp是一个DescribeDomainListResponse的实例，与请求对象对应
	response, err := client.DescribeDomainList(request)
	if err != nil {
		return err
	}

	if len(response.Response.DomainList) == 0 {
		return nil
	} else {
		for _, domain := range response.Response.DomainList {
			tencentDomainSlice = append(tencentDomainSlice, *domain.Name)
		}
	}

	return err
}

/**
* 查询腾讯云域名列表
 * @param *alidns.Client
 * @param *dnspod.Client
 * @return error
*/
func DescribeDomains(aliyunClient *alidns.Client, tencentClient *dnspod.Client) (_err error) {

	// 1.查询阿里云域名列表
	_err = AliyunDescribeDomains(aliyunClient)
	if _err != nil {
		setpStatusMap["aliyunDescribeDomainsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用阿里云域名列表接口:执行失败: %v", _err)
		panic(_err)
		// return _err
	} else {
		setpStatusMap["aliyunDescribeDomainsStatus"] = []string{successText, successColor}
		infologger.Printf("调用阿里云域名列表接口:执行完成")
	}

	//2.查询腾讯云域名列表
	_err = TencentDescribeDomains(tencentClient)
	if _err != nil {
		setpStatusMap["tencentDescribeDomainsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用腾讯云域名列表接口:执行失败: %v", _err)
		panic(_err)
		// return _err
	} else {
		setpStatusMap["tencentDescribeDomainsStatus"] = []string{successText, successColor}
		infologger.Printf("调用腾讯云域名列表接口:执行完成")
	}
	return _err
}

/**
* 查询阿里云域名解析记录
 * @param *alidns.Client
 * @param domains
 * @param domainRecordFile
 * @return error
*/
func AliyunDescribeDomainRecords(client *alidns.Client, domains *[]string, domainRecordFile *os.File) (_err error) {

	// 解引用指针获取实际的切片
	domainNames := *domains

	// 解引用指针获取实际的切片
	for _, domainName := range domainNames {
		// 定义初始页码和每页大小
		pageNumber := 1
		pageSize := 20

	InnerLoop:
		for {
			// 创建一个指向alidns.DescribeDomainsRequest类型结构体的指针，并初始化其成员变量PageNumber和PageSize
			// &alidns.DescribeDomainsRequest{}这部分创建了一个alidns.DescribeDomainsRequest类型的新实例，并且由于前面加了&，所以创建的是指向该实例的一个指针
			req := &alidns.DescribeDomainRecordsRequest{
				PageNumber: tea.Int64(int64(pageNumber)),
				PageSize:   tea.Int64(int64(pageSize)),
			}
			req.DomainName = &domainName

			resp, _err := client.DescribeDomainRecords(req)
			if _err != nil {
				return _err
			}

			if len(resp.Body.DomainRecords.Record) == 0 {
				// 使用 break outerLoop 跳出内层循环，继续外层循环
				break InnerLoop
			} else {
				for _, record := range resp.Body.DomainRecords.Record {
					if (*record.Type == "A" || *record.Type == "CNAME") &&
						*record.Status == "ENABLE" &&
						*record.RR != "@" &&
						*record.RR != "test" && *record.RR != "dev" &&
						!strings.Contains(*record.RR, ".test-") &&
						!strings.Contains(*record.RR, ".dev-") &&
						!strings.Contains(*record.RR, "-test-") &&
						!strings.Contains(*record.RR, "-dev-") &&
						!strings.Contains(*record.RR, "-test.") &&
						!strings.Contains(*record.RR, "-dev.") &&
						!strings.HasSuffix(*record.RR, "-test") &&
						!strings.HasSuffix(*record.RR, "-dev") &&
						!strings.HasPrefix(*record.RR, "test.") &&
						!strings.HasPrefix(*record.RR, "test-") &&
						!strings.HasPrefix(*record.RR, "dev.") &&
						!strings.HasPrefix(*record.RR, "dev-") {
						recordSlice = append(recordSlice, *record.RR+"."+*record.DomainName)
						// 将域名写入到文件中
						_, _err = domainRecordFile.WriteString(*record.RR + "." + *record.DomainName + "\n")
						if _err != nil {
							errlogger.Printf("写入domain.txt文件异常: %v", _err)
						}
					}
				}
				// 更新页码以获取下一页
				pageNumber++
			}
		}
	}
	return nil
}

/**
* 查询腾讯云域名解析记录
 * @param *dnspod.Client
 * @param domains
 * @param domainRecordFile
 * @return error
*/
func TencentDescribeDomainRecords(client *dnspod.Client, domains *[]string, domainRecordFile *os.File) (_err error) {

	// 解引用指针获取实际的切片
	domainNames := *domains

	var sum int = 0
	// 解引用指针获取实际的切片
	for _, domainName := range domainNames {
		req := dnspod.NewDescribeRecordListRequest()
		var offset uint64 = 0
		// 不用分页，默认显示3000条,最大的域名的所有记录578条
		var limit uint64 = 3000
		req.Offset = &offset
		req.Limit = &limit
		req.Domain = &domainName

		response, err := client.DescribeRecordList(req)
		if _, ok := err.(*errors.TencentCloudSDKError); ok {
			errlogger.Printf("调用腾讯云API返回错误: %v", _err)
			return
		}
		if err != nil {
			return err
		}

		for _, record := range response.Response.RecordList {
			sum++
			if (*record.Type == "A" || *record.Type == "CNAME") &&
				*record.Status == "ENABLE" &&
				*record.Name != "@" &&
				*record.Name != "test" && *record.Name != "dev" &&
				!strings.Contains(*record.Name, ".test-") &&
				!strings.Contains(*record.Name, ".dev-") &&
				!strings.Contains(*record.Name, "-test-") &&
				!strings.Contains(*record.Name, "-dev-") &&
				!strings.Contains(*record.Name, "-test.") &&
				!strings.Contains(*record.Name, "-dev.") &&
				!strings.HasSuffix(*record.Name, "-test") &&
				!strings.HasSuffix(*record.Name, "-dev") &&
				!strings.HasPrefix(*record.Name, "test.") &&
				!strings.HasPrefix(*record.Name, "test-") &&
				!strings.HasPrefix(*record.Name, "dev.") &&
				!strings.HasPrefix(*record.Name, "dev-") {
				recordSlice = append(recordSlice, *record.Name+"."+domainName)
				// 将域名写入到文件中
				_, _err = domainRecordFile.WriteString(*record.Name + "." + domainName + "\n")
				if _err != nil {
					errlogger.Printf("写入domain.txt文件异常: %v", _err)
				}
			}
		}
	}
	return nil
}

/**
* 查询域名解析记录
 * @param *alidns.Client
 * @param *dnspod.Client
 * @return error
*/
func DescribeDomainRecords(aliyunClient *alidns.Client, tencentClient *dnspod.Client) (_err error) {

	// 清空domain.txt 文件
	domainRecordFile, _err := os.OpenFile("domains.txt", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if _err != nil {
		errlogger.Printf("打开、创建或者清理domain.txt文件异常: %v", _err)
		return _err
	}
	defer domainRecordFile.Close()

	// 1.查询阿里云域名解析记录
	_err = AliyunDescribeDomainRecords(aliyunClient, &aliyunDomainSlice, domainRecordFile)
	if _err != nil {
		setpStatusMap["aliyunDescribeDomainRecordsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用阿里云域名解析接口:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["aliyunDescribeDomainRecordsStatus"] = []string{successText, successColor}
		infologger.Printf("调用阿里云域名解析接口:执行完成")
	}

	// 2.查询腾讯云域名解析记录
	_err = TencentDescribeDomainRecords(tencentClient, &tencentDomainSlice, domainRecordFile)
	if _err != nil {
		setpStatusMap["tencentDescribeDomainRecordsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用腾讯云域名解析接口:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["tencentDescribeDomainRecordsStatus"] = []string{successText, successColor}
		infologger.Printf("调用腾讯云域名解析接口:执行完成")
	}

	return nil
}

/**
* https域名检查过期时间
 * @return error
*/
func ExpirationHttpsDomain() (_err error) {

	// 清空httpsdomain.txt文件，用于存储https探测成功的域名
	domainFile, err := os.OpenFile("httpsdomain.txt", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		errlogger.Printf("打开、创建或者清理httpsdomain.txt文件异常: %v", err)
		return _err
	}
	defer domainFile.Close()

	// 清空template.yml文件，用于生成blackbox-exporter的配置文件（只部分片段）
	templateFile, err := os.OpenFile("aliyun-tencent-httpsdomain.yml", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		errlogger.Printf("打开、创建获取清理httpsdomain.yml文件异常: %v", err)
		return _err
	}
	defer templateFile.Close()

	// 打开文件
	file, err := os.Open("domains.txt")
	if err != nil {
		errlogger.Printf("打开domains.txt文件异常: %v", err)
		return _err
	}
	defer file.Close()

	// 创建一个新的scanner来读取文件
	scanner := bufio.NewScanner(file)

	// 用于等待一组并发操作完成
	var wg sync.WaitGroup
	// 用于保护对domainFile和templateString的并发访问
	var mutex sync.Mutex

	// 初始化模板字符串
	var templateString strings.Builder
	templateString.WriteString("- targets:\n") // 开始模板字符串

	// 匿名函数，用于并发，后面go processDomain(domain)调用
	processDomain := func(domain string) {
		// 匿名函数退出的时候执行，wg.Done()方法用于减少等待组的计数器。一个goroutine完成时，应调用wg.Done()来通知等待组告知完成。这有助于sync.WaitGroup能够正确地跟踪还有多少个goroutine正在运行，以及是否所有的goroutine都已经完成
		defer wg.Done()

		// 创建TCP连接探测443端口是否通，异常记录错误日志
		conn, err := net.DialTimeout("tcp", domain+":443", 5*time.Second)
		if err != nil {
			errlogger.Printf("连接异常 %s: %v", domain, err)
			return
		}
		defer conn.Close()

		// 创建TLS配置并启动TLS握手，异常记录错误日志
		tlsConfig := &tls.Config{
			ServerName:         domain,
			InsecureSkipVerify: false,
		}
		tlsConn := tls.Client(conn, tlsConfig)
		err = tlsConn.Handshake()
		if err != nil {
			errlogger.Printf("TLS handshake异常 %s: %v", domain, err)
			return
		}

		// 获取连接状态并提取证书，异常记录错误日志
		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			errlogger.Printf("提取证书异常 %s", domain)
			return
		}

		// 获取第一个证书（通常是叶子证书）
		cert := state.PeerCertificates[0]

		// 获取证书到期时间
		expiration := cert.NotAfter
		infologger.Printf("Certificate for %s expires on: %s\n", domain, expiration)

		// 使用互斥锁来保护对文件的写入，将域名写入到文件中
		mutex.Lock()
		_, err = domainFile.WriteString(domain + "\n")
		if err != nil {
			errlogger.Printf("写入域名异常 %s to file: %v", domain, err)
		}
		// 累加https成功域名的数量
		httpsDomainSum++

		// 构造新的目标条目并追加到模板字符串
		templateString.WriteString(fmt.Sprintf("   - https://%s\n", domain))
		mutex.Unlock()
	}

	// 遍历每一行，多少行启动多少个线程，一般最好声明数量
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain == "" {
			continue // 跳过空行
		}

		// 在启动一个新的goroutine之前调用它，以表示有一个新的goroutine将执行，并需要在之后的某个时刻等待其完成。
		wg.Add(1)
		go processDomain(domain)
	}

	// 检查读取过程中是否出错
	if err := scanner.Err(); err != nil {
		errlogger.Printf("读取文件异常: %v", err)
	}

	// 用于阻塞调用它的goroutine，直到等待组的计数器变为零。这通常意味着所有添加到等待组的goroutine都已经通过调用wg.Done()完成了它们的工作
	wg.Wait()

	// 添加 labels 部分
	templateString.WriteString("  labels:\n")
	templateString.WriteString("    group: web\n")
	templateString.WriteString("    department: test-auto\n")

	// 将模板字符串写入到文件中
	_, err = templateFile.WriteString(templateString.String())
	if err != nil {
		errlogger.Printf("写入template.yml文件异常: %v", err)
	}
	return nil
}

/**
* 调用Prometheus Reload接口
 * @return error
*/
func ReloadPrometheus() (_err error) {
	// 要请求的URL
	url := viper.GetString("api.prometheus_api")

	// 创建一个空的POST请求
	resp, _err := http.Post(url, "application/json", nil)
	if _err != nil {
		errlogger.Printf("请求异常: %v", _err)
		return _err
	}
	defer resp.Body.Close()

	// 读取响应体
	body, _err := ioutil.ReadAll(resp.Body)
	if _err != nil {
		errlogger.Printf("响应异常: %v", _err)
		return _err
	}

	// 打印响应状态码和响应体
	infologger.Printf("调用Prometheus Reload接口状态码: %s", resp.Status)
	infologger.Printf("调用Prometheus Reload接口出参: %s", string(body))
	return nil
}

/**
* 执行结果发送通知
 * @param httpsDomainSum
 * @param setpStatusMap
 * @return error
*/
func NoticeWeCom(httpsDomainSum int, setpStatusMap map[string][]string) (_err error) {

	// 企业微信Webhook URL
	url := viper.GetString("api.wx_api")

	// 准备Markdown消息内容
	markdownMessage := func() string {
		aliyunInitStatus := setpStatusMap["aliyunInitStatus"]
		aliyunDescribeDomainsStatus := setpStatusMap["aliyunDescribeDomainsStatus"]
		aliyunDescribeDomainRecordsStatus := setpStatusMap["aliyunDescribeDomainRecordsStatus"]
		tencentInitStatus := setpStatusMap["tencentInitStatus"]
		tencentDescribeDomainsStatus := setpStatusMap["tencentDescribeDomainsStatus"]
		tencentDescribeDomainRecordsStatus := setpStatusMap["tencentDescribeDomainRecordsStatus"]
		expirationHttpsDomainStatus := setpStatusMap["expirationHttpsDomainStatus"]
		reloadPrometheusStatus := setpStatusMap["reloadPrometheusStatus"]

		content := fmt.Sprintf(`本次已同步HTTPS域名<font color="yellow">%d条</font>，请相关同事注意。

		> 【阿里云】
		> 1、初始化阿里云SDK: <font color="%s">%s</font>
		> 2、调用阿里云域名列表接口: <font color="%s">%s</font>
		> 3、调用阿里云域名解析接口: <font color="%s">%s</font>

		> 【腾讯云】
		> 1、初始化腾讯云SDK: <font color="%s">%s</font>
		> 2、调用腾讯云域名列表接口: <font color="%s">%s</font>
		> 3、调用腾讯云域名解析接口: <font color="%s">%s</font>

		> 【HTTPS域名检查】
		> 4、检查HTTPS域名到期时间: <font color="%s">%s</font>
		> 5、Reload状态: <font color="%s">%s</font>`,
			httpsDomainSum,
			aliyunInitStatus[1], aliyunInitStatus[0],
			aliyunDescribeDomainsStatus[1], aliyunDescribeDomainsStatus[0],
			aliyunDescribeDomainRecordsStatus[1], aliyunDescribeDomainRecordsStatus[0],
			tencentInitStatus[1], tencentInitStatus[0],
			tencentDescribeDomainsStatus[1], tencentDescribeDomainsStatus[0],
			tencentDescribeDomainRecordsStatus[1], tencentDescribeDomainRecordsStatus[0],
			expirationHttpsDomainStatus[1], expirationHttpsDomainStatus[0],
			reloadPrometheusStatus[1], reloadPrometheusStatus[0])

		return content
	}

	// 准备Markdown消息内容
	message := MarkdownMessage{
		MsgType: "markdown",
		Markdown: struct {
			Content string `json:"content"`
		}{
			Content: markdownMessage(),
		},
	}

	// 将消息内容序列化为JSON格式
	messageBytes, _err := json.Marshal(message)
	if _err != nil {
		errlogger.Printf("序列化JSON异常: %v", _err)

		return _err
	}

	// 创建一个HTTP请求
	req, _err := http.NewRequest("POST", url, bytes.NewBuffer(messageBytes))
	if _err != nil {
		errlogger.Printf("请求异常: %v", _err)
		return _err
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 创建一个HTTP客户端来发送请求
	client := &http.Client{}

	// 发送请求并获取响应
	resp, _err := client.Do(req)
	if _err != nil {
		errlogger.Printf("请求异常: %v", _err)
		return _err
	}
	defer resp.Body.Close()

	// 读取响应体
	body, _err := ioutil.ReadAll(resp.Body)
	if _err != nil {
		errlogger.Printf("响应异常: %v", _err)
		return _err
	}

	// 打印响应状态码和响应体
	infologger.Printf("调用企业微信接口状态码: %s", resp.Status)
	infologger.Printf("调用企业微信接口出参: %s", string(body))
	return nil
}

/**
* 主要执行入口(被main主函数调用)
 * @return error
*/
func _main() (_err error) {

	// 匿名函数：退出之前发送通知
	defer func() (_err error) {
		_err = NoticeWeCom(httpsDomainSum, setpStatusMap)
		if _err != nil {
			errlogger.Printf("发送通知失败: %v", _err)
			return _err
		} else {
			infologger.Printf("发送通知成功")
		}
		return nil
	}()

	// 加载配置文件
	_err = GetConfig()
	if _err != nil {
		errlogger.Printf("加载配置文件失败: %v", _err)
		return _err
	} else {
		infologger.Printf("加载配置文件成功")
	}

	// 1、初始化阿里云和腾讯云SDK
	aliyunClient, tencentClient, _err := ClientInit()
	if _err != nil {
		setpStatusMap["initStatus"] = []string{failText, failColor}
		errlogger.Printf("调用域名列表接口:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["initStatus"] = []string{successText, successColor}
		infologger.Printf("调用域名列表接口:执行完成")
	}

	// 2、查询域名列表
	_err = DescribeDomains(aliyunClient, tencentClient)
	if _err != nil {
		setpStatusMap["describeDomainsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用域名列表接口:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["describeDomainsStatus"] = []string{successText, successColor}
		infologger.Printf("调用域名列表接口:执行完成")
	}

	// 3.查询域名解析记录
	_err = DescribeDomainRecords(aliyunClient, tencentClient)
	if _err != nil {
		setpStatusMap["describeDomainRecordsStatus"] = []string{failText, failColor}
		errlogger.Printf("调用域名解析接口:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["describeDomainRecordsStatus"] = []string{successText, successColor}
		infologger.Printf("调用域名解析接口:执行完成")
	}

	// 4.检查https域名到期时间
	// 让程序暂停 3 秒
	time.Sleep(3 * time.Second)
	_err = ExpirationHttpsDomain()
	if _err != nil {
		setpStatusMap["expirationHttpsDomainStatus"] = []string{failText, failColor}
		errlogger.Printf("检查HTTPS域名到期时间:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["expirationHttpsDomainStatus"] = []string{successText, successColor}
		infologger.Printf("检查HTTPS域名到期时间:执行完成")
	}

	// 5.Reload Prometheus
	// 让程序暂停 3 秒
	time.Sleep(3 * time.Second)
	_err = ReloadPrometheus()
	if _err != nil {
		setpStatusMap["reloadPrometheusStatus"] = []string{failText, failColor}
		errlogger.Printf("Reload状态:执行失败: %v", _err)
		return _err
	} else {
		setpStatusMap["reloadPrometheusStatus"] = []string{successText, successColor}
		infologger.Printf("Reload状态:执行完成")
	}

	return _err
}

func main() {
	// 关闭日志文件
	defer preClose()

	// 由于_main先执行，它返回错误就会中断程序
	err := _main()
	if err != nil {
		panic(err)
	}
}
