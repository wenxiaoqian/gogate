package serv

import (
	"errors"
	"strconv"
	"strings"

	"github.com/alecthomas/log4go"
	"github.com/valyala/fasthttp"
	"github.com/wanghongfei/gogate/utils"
)

const (
	SERVICE_NAME = "key_service_name"
	REQUEST_PATH = "key_request_path"
	ROUTE_INFO = "key_route_info"

	RELOAD_PATH = "/_mgr/reload"
)

// HTTP请求处理方法
func (s *Server) HandleRequest(ctx *fasthttp.RequestCtx) {
	defer func() {
		if r := recover(); r != nil {
			log4go.Error(r)
			processPanic(ctx)
		}
	}()

	// 取出请求path
	path := string(ctx.Path())
	ctx.SetUserValue(REQUEST_PATH, path)

	log4go.Info("request received: %s %s", string(ctx.Method()), path)

	// 处理reload请求
	if path == RELOAD_PATH {
		err := s.ReloadRoute()
		if nil != err {
			log4go.Error(err)
			NewResponse(path, err.Error()).Send(ctx)
			return
		}

		ctx.WriteString(s.ExtractRoute())
		return
	}

	newReq := new(fasthttp.Request)
	ctx.Request.CopyTo(newReq)

	// 调用Pre过虑器
	ok := invokePreFilters(s, ctx, newReq)
	if !ok {
		return
	}

	// 发请求
	sw := utils.NewStopwatch()
	resp, err := s.sendRequest(ctx, newReq)
	if nil != err {
		log4go.Error(err)
		NewResponse(path, err.Error()).Send(ctx)
		return
	}
	resp.Header.Add("Time", strconv.FormatInt(sw.Record(), 10))

	// 调用Post过虑器
	ok = invokePostFilters(s.postFilters, newReq, resp)
	if !ok {
		return
	}

	// 返回响应
	sendResponse(ctx, resp)
}

func sendResponse(ctx *fasthttp.RequestCtx, resp *fasthttp.Response) {
	// copy header
	ctx.Response.Header = resp.Header
	ctx.Response.Header.Add("proxy", "gogate")

	ctx.Write(resp.Body())
}

func (s *Server) sendRequest(ctx *fasthttp.RequestCtx, req *fasthttp.Request) (*fasthttp.Response, error) {
	// 获取服务信息
	info := ctx.UserValue(ROUTE_INFO).(*ServiceInfo)

	var c *fasthttp.LBClient
	// 需要从注册列表中查询地址
	if info.Id != "" {
		// 获取Client
		appId := info.Id
		client, exist := s.proxyClients.Load(appId)
		if !exist {
			return nil, errors.New("no client for service " + appId)
		}

		c = client.(*fasthttp.LBClient)

	} else {
		// 直接使用后面的地址
		hostList := strings.Split(info.Host, ",")
		c = &fasthttp.LBClient{
			Clients: createClients(hostList),
		}
	}



	// 发请求
	resp := new(fasthttp.Response)
	err := c.Do(req, resp)
	if nil != err {
		return nil, err
	}

	return resp, nil
}

func invokePreFilters(s *Server, ctx *fasthttp.RequestCtx, newReq *fasthttp.Request) bool {
	for _, f := range s.preFilters {
		next := f(s, ctx, newReq)
		if !next {
			return false
		}
	}

	return true
}

func invokePostFilters(filters []PostFilterFunc, newReq *fasthttp.Request, resp *fasthttp.Response) bool {
	for _, f := range filters {
		next := f(newReq, resp)
		if !next {
			return false
		}
	}

	return true
}

func processPanic(ctx *fasthttp.RequestCtx) {
	path := string(ctx.Path())
	NewResponse(path, "system error").SendWithStatus(ctx, 500)
}
