package smartWechat

import (
	"net/http"
	"fmt"
	"encoding/xml"
	"time"
	"encoding/json"
	"strings"
	"os"
	"io/ioutil"
	"regexp"
	"strconv"
	"github.com/beewit/wechat-ai/enum"
	"errors"
	"encoding/base64"
	"github.com/beewit/beekit/utils/convert"
)

/* 从微信服务器获取登陆uuid */
func GetUUIDFromWX() (string, error) {
	paraMap := enum.GetUUIDParaEnum()
	paraMap[enum.TimeStamp] = fmt.Sprintf("%d", time.Now().Unix())

	resp, err := http.Get(enum.UUID_URL + GetURLParams(paraMap))
	if err != nil {
		return "", errors.New("访问微信服务器API失败:" + err.Error())
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.New("获取微信API反馈UUID数据失败:" + err.Error())
	}

	/* 正则解析uuid ,FindStringSubmatch首先返回整体匹配结果，然后返回每个子表达式的匹配结果*/
	reg := regexp.MustCompile(`^window.QRLogin.code = (\d+); window.QRLogin.uuid = "(\S+)";$`)
	matches := reg.FindStringSubmatch(string(bodyBytes))
	if len(matches) != 3 {
		return "", errors.New("解析微信UUID API数据失败:" + string(bodyBytes))
	}
	status, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return "", errors.New("解析微信UUID API数据失败:" + err.Error())
	}

	if status != 200 {
		return "", errors.New(fmt.Sprintf("微信返回的状态错误，请排查网络故障，如仍有问题，可能是微信更改了API，请联系开发者。status:%d", status))
	}

	return matches[2], nil
}

/* 下载URL指向的JPG base64*/
func DownloadImage(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	base64Img := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
	return base64Img, err
}

/* 判断微信是否登陆 */
func CheckLogin(uuid string) (int64, string) {
	var timestamp int64 = time.Now().UnixNano() / 1000000
	paraMap := enum.GetLoginParaEnum()
	paraMap[enum.UUID] = uuid
	paraMap[enum.TimeStamp] = fmt.Sprintf("%d", timestamp)
	paraMap[enum.R] = fmt.Sprintf("%d", ^(int32)(timestamp))
	var paraStr = GetURLParams(paraMap)

	resp, err := http.Get(enum.LOGIN_URL + paraStr)
	if err != nil {
		return 0, "访问微信服务器API失败:" + err.Error()
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, "获取微信API反馈登陆数据失败:" + err.Error()
	}

	var resultStr = string(bodyBytes)

	reg := regexp.MustCompile(`^window.code=(\d+);`)
	matches := reg.FindStringSubmatch(resultStr)
	if len(matches) < 2 {
		return 0, "预期的返回结果格式不匹配"
	}

	status, err := strconv.ParseInt(matches[1], 10, 64)

	return status, resultStr
}

/**
 * 处理微信登陆成功时返回的其他登陆数据
 * 首先根据回调URI再次Get一次微信服务器，得到XML格式的登陆数据
 * 解析XML，向map中压入相关数据
 */
func ProcessLoginInfo(loginInfoStr string) (*WechatClient, error) {
	resultMap := WechatClient{}
	reg := regexp.MustCompile(`window.redirect_uri="(\S+)";`)
	matches := reg.FindStringSubmatch(loginInfoStr)
	if len(matches) < 2 {
		return &resultMap, errors.New("登陆反馈的信息格式有误")
	}

	/* https://wx.qq.com/cgi-bin/mmwebwx-bin/webwxnewloginpage?ticket=AQ2uT-dEQQWVcwTg_oiY2UYl@qrticket_0&uuid=gb2NHSWMLg==&lang=zh_CN&scan=1503967665 */
	orginUri := matches[1] + "&fun=new&version=v2"

	/* 这里除了XML的返回之外，还会有一些Cookie数据传给客户端，需要收集起来 */
	resp, err := http.Get(orginUri)
	if err != nil {
		return &resultMap, errors.New("访问微信登陆回调URL有误" + err.Error())
	}
	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return &resultMap, errors.New("获取微信登陆回调URL数据失败:" + err.Error())
	}
	println("登录webwxnewloginpage：", string(bodyBytes))

	loginCallbackXMLResult := LoginCallbackXMLResult{}
	err = xml.Unmarshal(bodyBytes, &loginCallbackXMLResult)

	resultMap.BaseRequest.SKey = loginCallbackXMLResult.SKey
	resultMap.BaseRequest.Sid = loginCallbackXMLResult.WXSid
	resultMap.BaseRequest.Uin = loginCallbackXMLResult.WXUin
	resultMap.BaseRequest.DeviceID = "e" + GetRandomString(10, 15)

	resultMap.PassTicket = loginCallbackXMLResult.PassTicket

	/* 收集Cookie */
	resultMap.Cookies = resp.Cookies()

	return &resultMap, nil
}

/* 初始化微信登陆数据 */
func InitWX(loginMap *WechatClient) error {
	/* post URL */
	var urlMap = enum.GetInitParaEnum()
	var timestamp int64 = time.Now().UnixNano() / 1000000
	urlMap[enum.R] = fmt.Sprintf("%d", ^(int32)(timestamp))
	urlMap[enum.PassTicket] = loginMap.PassTicket
	urlMap[enum.Lang] = enum.LangValue

	/* post数据 */
	initPostJsonData := map[string]interface{}{}
	initPostJsonData["BaseRequest"] = loginMap.BaseRequest

	jsonBytes, err := json.Marshal(initPostJsonData)
	if err != nil {
		return err
	}
	println(string(jsonBytes))
	url := enum.INIT_URL + GetURLParams(urlMap)
	println(url)
	resp, err := http.Post(url, enum.JSON_HEADER, strings.NewReader(string(jsonBytes)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	println("微信初始化数据：" + string(bodyBytes))
	initInfo := InitInfo{}
	err = json.Unmarshal(bodyBytes, &initInfo)
	if err != nil {
		return errors.New("无法解析JSON至InitInfo对象:" + err.Error() + "，bodyBytes：" + string(bodyBytes))
	}
	loginMap.InitInfo = &initInfo
	loginMap.SelfNickName = initInfo.User.NickName
	loginMap.SelfUserName = initInfo.User.UserName

	println("initInfo", convert.ToObjStr(initInfo))

	/* 组装synckey */
	if initInfo.SyncKeys.Count < 1 {
		return errors.New("微信返回的数据有误，SyncKeys未获取到！，bodyBytes：" + string(bodyBytes))
	}
	loginMap.SyncKeys = initInfo.SyncKeys
	loginMap.SyncKeyStr = initInfo.SyncKeys.ToString()

	return nil
}

/**
 * 通知微信服务器状态变化，只要通知即可，无需处理返回数据
 * {"BaseRequest":{"Uin":154158775,"Sid":"/nxZxJ0LclxmOw8v","Skey":"@crypt_3aaab8d5_cdfa952ec95e594b100f44aba942a73c","DeviceID":"e390742104557152"},"Code":3,"FromUserName":"@fc96d593487db4fb92b9a633aec8293b","ToUserName":"@fc96d593487db4fb92b9a633aec8293b","ClientMsgId":1504571331980}
 */
func NotifyStatus(loginMap *WechatClient) error {
	urlMap := map[string]string{
		enum.PassTicket: loginMap.PassTicket}

	statusNotifyJsonData := map[string]interface{}{}
	statusNotifyJsonData["BaseRequest"] = loginMap.BaseRequest
	statusNotifyJsonData["Code"] = 3
	statusNotifyJsonData["FromUserName"] = loginMap.SelfUserName
	statusNotifyJsonData["ToUserName"] = loginMap.SelfUserName
	statusNotifyJsonData["ClientMsgId"] = time.Now().UnixNano() / 1000000

	jsonBytes, err := json.Marshal(statusNotifyJsonData)
	if err != nil {
		return err
	}

	//fmt.Println("notify json:\n" + string(jsonBytes))

	_, err = http.Post(enum.STATUS_NOTIFY_URL+GetURLParams(urlMap), enum.JSON_HEADER, strings.NewReader(string(jsonBytes)))

	return err
}

func isDirExist(path string) bool {
	p, err := os.Stat(path)
	if err != nil {
		return os.IsExist(err)
	} else {
		return p.IsDir()
	}
}

/**
 * 附录：
 * 微信登陆后回调URL的XML返回结果：
 * <error>
 *   <ret>0</ret>
 *   <message></message>
 *   <skey>@crypt_3aaab8d5_aa9febb1c57122a4569c1b1dc4772eac</skey>
 *   <wxsid>vjqCszEkQQw9jep1</wxsid>
 *   <wxuin>154158775</wxuin>
 *   <pass_ticket>wbFO7Vqg%2BpADuIcrQPDM1e0KjmNvgsH8jYAEoT0FtSY%3D</pass_ticket>
 *   <isgrayscale>1</isgrayscale>
 * </error>
 *
 */
