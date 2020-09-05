package selenium

import (
	"fmt"
	"github.com/yudeguang/file"
	"github.com/yudeguang/selenium/chrome"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

//用于对selenium的扩展,该包里的remoteWD没有导出，需要对其做部分的修改，所以放一个扩展文件到这里
//用于从当前目录的webdriver.session文件中读取原有的session,用这个session做操作
type ChromeExtendWebDriver struct {
	*remoteWD
	LastPageTitle string
	ProxyDataDir  string //代理服务器的目录地址 .../temp_proxy_data
}

//代理服务器截获的数据
type ProxyData struct {
	Data     string //数据内容
	FileName string //文件名
	//General         string
	RequestMethod string //从Request中拆分出来，方便跟踪
	RequestURL    string //从Request中拆分出来，方便跟踪
	Request       string //请求头
	Response      string //回复头
	HTML          string //正式的返回内容
}

//参数
//useHttpProxy:是否使用本地代理,端口地址是:127.0.0.1:9005
//args[0]: capabilities->selenium.Capabilities
//args[1]: urlPrefix->string
func NewChromeFromFileWithProxy(args ...interface{}) (*ChromeExtendWebDriver, error) {
	return newChromeSessionFromFile("127.0.0.1:9005", args...)
}
func NewChromeFromFileNoProxy(args ...interface{}) (*ChromeExtendWebDriver, error) {
	return newChromeSessionFromFile("", args...)
}

func newChromeSessionFromFile(proxyAddr string, args ...interface{}) (*ChromeExtendWebDriver, error) {
	var oldSessionId = ""
	var oldSessionFile = ""
	f, err := os.Executable()
	if err != nil {
		return nil, err
	}
	//获取
	ProxyDataDir, err := getProxyDataDir()
	if err != nil {
		return nil, err
	}
	oldSessionFile = filepath.Join(filepath.Dir(f), "webdriver.session")
	data, err := ioutil.ReadFile(oldSessionFile)
	if err == nil {
		oldSessionId = string(data)
	}
	oldSessionId = strings.TrimSpace(oldSessionId)
	//准备一些默认的参数,这里外面就可以不传了,直接默认掉
	var capabilities Capabilities = nil
	if len(args) > 0 {
		c, ok := args[0].(Capabilities)
		if ok {
			capabilities = c
		}
	}
	if capabilities == nil {
		prefs := make(map[string]interface{}) //改为默认加载图片，免得有时候会用到
		// prefs := map[string]interface{}{ //禁止图片加载，加快渲染速度
		// 	"profile.managed_default_content_settings.images": 2,
		// }
		chromeCaps := chrome.Capabilities{
			Prefs:        prefs,
			DebuggerAddr: "",
		}
		//判断是否使用代理
		if proxyAddr != "" { //加入默认的端口代理,9005
			chromeCaps.Args = []string{"/proxy-server=http://" + proxyAddr}
		}
		chromeCaps.ExcludeSwitches = []string{"enable-automation"}
		capabilities = Capabilities{"browserName": "chrome"}
		capabilities.AddChrome(chromeCaps)
	}

	//判断urlPrefix
	var urlPrefix = DefaultURLPrefix
	if len(args) > 1 {
		c, ok := args[1].(string)
		if ok {
			urlPrefix = c
		}
	}
	//参数准备完了,开始判断
	if oldSessionId != "" {
		//用原有的session创建,尝试用下原有的看看可不可以
		wd := &remoteWD{
			urlPrefix:    urlPrefix,
			capabilities: capabilities,
		}
		if b := capabilities["browserName"]; b != nil {
			wd.browser = b.(string)
		}
		wd.SwitchSession(oldSessionId)
		//尝试获取title,如果获取不到,认为有问题了,关闭掉
		title, err := wd.Title()
		if err == nil { //这个可以用,就使用这个
			//log.Println("获取到标题为:",title)
			extDriver := &ChromeExtendWebDriver{
				remoteWD:      wd,
				LastPageTitle: title,
				ProxyDataDir:  ProxyDataDir,
			}
			return extDriver, nil
		}
		os.Remove(oldSessionFile)
		wd.Quit()
	}
	//到这里的话就说明原有的session不可用了,那么就新创建一个
	wd, err := NewRemote(capabilities, urlPrefix)
	if err != nil {
		return nil, err
	}
	ioutil.WriteFile(oldSessionFile, []byte(wd.SessionID()), 644)
	extDriver := &ChromeExtendWebDriver{
		remoteWD:     wd.(*remoteWD),
		ProxyDataDir: ProxyDataDir,
	}

	return extDriver, nil
}

//通过HTTP方式获得代理服务器临时目录的物理地址
func getProxyDataDir() (string, error) {
	resp, err := http.Get("http://127.0.0.1:9006/programadress")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if string(body) == "" {
		err = fmt.Errorf("programadress not find")
	}
	return filepath.Join(string(body), "temp_proxy_data"), nil
}

// //代理服务器存储数据的临时目录地址，如果后续需要获取网页head等内容，需要增加此属性,一般是自动实现
// func (this *ChromeExtendWebDriver) SetProxyDataDir(ProxyDataDir string) {
// 	this.ProxyDataDir = ProxyDataDir
// }

//检察ProxyDataDir是否设置正确
func (this *ChromeExtendWebDriver) checktProxyDataDir() error {
	if this.ProxyDataDir == "" {
		return fmt.Errorf("请使用SetProxyDataDir函数指定代理服务临时地址")
	}
	if !file.Exist(this.ProxyDataDir) {
		return fmt.Errorf("代理服务临时地址指定错误:" + this.ProxyDataDir)
	}
	return nil
}

//删除旧文件,一般在请求某个URL之前调用此函数,以防止后续获取数据时获取到的数据混杂先前其它请求的数据
func (this *ChromeExtendWebDriver) DeleteOldHTML(URL string) error {
	err := this.checktProxyDataDir()
	if err != nil {
		return err
	}
	u, err := url.Parse(URL)
	if err != nil {
		return err
	}
	fileNames, err := file.GetFileListJustCurrentDirBySuffix(filepath.Join(this.ProxyDataDir, u.Host), ".txt")
	if err != nil {
		return err
	}
	for _, fileName := range fileNames {
		err = os.Remove(fileName)
		if err != nil {
			return err
		}
	}
	return nil
}

//访问某个URL之后，获取访问该URL对应的所有通信数据，有可能是多个结果
func (this *ChromeExtendWebDriver) GetCurProxyDatas(URL string) (data []ProxyData, err error) {
	err = this.checktProxyDataDir()
	if err != nil {
		return
	}
	u, err := url.Parse(URL)
	if err != nil {
		return
	}
	fileNames, err := file.GetFileListJustCurrentDirBySuffix(filepath.Join(this.ProxyDataDir, u.Host), ".txt")
	if err != nil {
		return
	}
	for _, fileName := range fileNames {
		b, err := ioutil.ReadFile(fileName)
		if err == nil {
			arr := strings.Split(string(b), `------HTTP_PROXY_SPLIT------`)
			var RequestMethod, RequestURL, Request, Response, HTML string
			if len(arr) == 3 {
				Request = arr[0]
				Response = arr[1]
				HTML = arr[2]
				//数据用换行进行分隔
				arr2 := strings.Split(strings.Split(Request, "\r\n")[0], " ")
				if len(arr2) == 3 {
					RequestMethod = arr2[0]
					RequestURL = arr2[1]
				}
			}
			data = append(data, ProxyData{string(b), file.FileName(fileName), RequestMethod, RequestURL, Request, Response, HTML})
		}
	}
	if len(data) == 0 {
		err = fmt.Errorf("未能成功获取数据")
	}
	//从大到小排序
	sort.Slice(data, func(i, j int) bool {
		return data[i].FileName < data[i].FileName
	})
	return
}

/*
从header或其它地方拆分出所需要的内容
Accept-Encoding: gzip, deflate, br
Accept-Language: zh-CN,zh;q=0.9
Connection: keep-alive
Content-Length: 885
Content-Type: text/plain;charset=UTF-8
Cookie: dtCookie=-13$K1KGLL0A5K59O4D3ERSVD4EM2T5GGT80
*/
func (this *ChromeExtendWebDriver) GetValFrom(RequestOrResponse string, key string) string {
	equal, newLine := ": ", "\r\n"
	if RequestOrResponse != "" {
		for _, line := range strings.Split(RequestOrResponse, newLine) {
			pos := strings.Index(line, equal)
			if pos > 0 {
				curKey := line[:pos]
				if curKey == key {
					return line[pos+1:]
				}
			}
		}
	}
	return ""
}

//ChromeExtendWebDriver的扩展函数
//带超时时间的获取某个id,如果timeoutsec为n的话是表示需要获取n+1次
func (this *ChromeExtendWebDriver) FindElementTimeout(findby string, name string, timeoutsec int) (WebElement, error) {
	if timeoutsec <= 0 {
		return this.FindElement(findby, name)
	}
	//秒*2每次延迟500ms
	for i := 0; i <= timeoutsec*2; i++ {
		obj, err := this.FindElement(findby, name)
		if err == nil {
			return obj, err
		}
		time.Sleep(time.Millisecond * 500)
	}
	return nil, fmt.Errorf("find elemtent[%s:%s] timeout:%d s", findby, name, timeoutsec)
}

//SetElementValue设置某个ID的值,args第一个参数,表示需要循环等待获取多少时间,不传的话就获取到立即设置一下就可以
func (this *ChromeExtendWebDriver) SetElementValue(findby string, name string, value string, args ...int) error {
	var nwaitsecond = 0
	if len(args) > 0 && args[0] > 0 {
		nwaitsecond = args[0]
	}
	obj, err := this.FindElementTimeout(findby, name, nwaitsecond)
	if err != nil {
		return err
	}
	return obj.SendKeys(value)
}
func (this *ChromeExtendWebDriver) SetElementValueById(name string, value string, args ...int) error {
	return this.SetElementValue(ByID, name, value, args...)
}
func (this *ChromeExtendWebDriver) SetElementValueByName(name string, value string, args ...int) error {
	return this.SetElementValue(ByName, name, value, args...)
}

//点击某个元素,args同样是可以等待这么长时间来获取
func (this *ChromeExtendWebDriver) ClickElement(findby string, name string, args ...int) error {
	var nwaitsecond = 0
	if len(args) > 0 && args[0] > 0 {
		nwaitsecond = args[0]
	}
	obj, err := this.FindElementTimeout(findby, name, nwaitsecond)
	if err != nil {
		return err
	}
	return obj.Click()
}
func (this *ChromeExtendWebDriver) ClickElementById(name string, args ...int) error {
	return this.ClickElement(ByID, name, args...)
}
func (this *ChromeExtendWebDriver) ClickElementByName(name string, args ...int) error {
	return this.ClickElement(ByName, name, args...)
}

//获得body的文本内容
func (this *ChromeExtendWebDriver) GetBodyText() (string, error) {
	body, err := this.FindElement(ByTagName, "body")
	if err != nil {
		return "", err
	}
	return body.Text()
}
