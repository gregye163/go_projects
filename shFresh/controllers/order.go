package controllers

import (
	"github.com/astaxie/beego"
	"strconv"
	"github.com/astaxie/beego/orm"
	"shFresh/models"
	"github.com/gomodule/redigo/redis"
	"time"
	"strings"
	"fmt"
	"github.com/smartwalle/alipay"
)

type OrderController struct {
	beego.Controller
}

func(this*OrderController)ShowOrder(){
	//获取数据
	skuids :=this.GetStrings("skuid")
	beego.Info(skuids)
	//校验数据
	if len(skuids) == 0{
		beego.Info("请求数据错误")
		this.Redirect("/user/cart",302)
		return
	}

	//处理数据
	o := orm.NewOrm()
	conn,_ := redis.Dial("tcp","192.168.110.81:6379")
	defer conn.Close()
	//获取用户数据
	var user models.User
	userName := this.GetSession("userName")
	user.Name = userName.(string)
	o.Read(&user,"Name")

	goodsBuffer := make([]map[string]interface{},len(skuids))

	totalPrice := 0
	totalCount := 0
	for index,skuid := range skuids{
		temp := make(map[string]interface{})

		id ,_ := strconv.Atoi(skuid)
		//查询商品数据
		var goodsSku models.GoodsSKU
		goodsSku.Id = id
		o.Read(&goodsSku)

		temp["goods"] = goodsSku
		//获取商品数量
		count,_ :=redis.Int(conn.Do("hget","cart_"+strconv.Itoa(user.Id),id))
		temp["count"] = count
		//计算小计
		amount := goodsSku.Price * count
		temp["amount"] = amount

		//计算总金额和总件数
		totalCount += count
		totalPrice += amount

		goodsBuffer[index] = temp
	}

	this.Data["goodsBuffer"] = goodsBuffer

	//获取地址数据
	var addrs []models.Address
	o.QueryTable("Address").RelatedSel("User").Filter("User__Id",user.Id).All(&addrs)
	this.Data["addrs"] = addrs

	//传递总金额和总件数
	this.Data["totalPrice"] = totalPrice
	this.Data["totalCount"] = totalCount
	transferPrice := 10
	this.Data["transferPrice"] = transferPrice
	this.Data["realyPrice"] = totalPrice + transferPrice

	//传递所有商品的id
	this.Data["skuids"] = skuids

	//返回视图
	this.TplName = "place_order.html"
}

//添加订单
func(this*OrderController)AddOrder(){
	//获取数据
	addrid,_ :=this.GetInt("addrid")
	payId,_ :=this.GetInt("payId")
	skuid := this.GetString("skuids")
	ids := skuid[1:len(skuid)-1]

	skuids := strings.Split(ids," ")


	beego.Error(skuids)
	//totalPrice,_ := this.GetInt("totalPrice")
	totalCount,_ := this.GetInt("totalCount")
	transferPrice,_ :=this.GetInt("transferPrice")
	realyPrice,_:=this.GetInt("realyPrice")


	resp := make(map[string]interface{})
	defer this.ServeJSON()
	//校验数据
	if len(skuids) == 0{
		resp["code"] = 1
		resp["errmsg"] = "数据库链接错误"
		this.Data["json"] = resp
		return
	}
	//处理数据
	//向订单表中插入数据
	o := orm.NewOrm()

	o.Begin()//标识事务的开始

	userName := this.GetSession("userName")
	var user models.User
	user.Name = userName.(string)
	o.Read(&user,"Name")

	var order models.OrderInfo
	order.OrderId = time.Now().Format("2006010215030405")+strconv.Itoa(user.Id)
	order.User = &user
	order.Orderstatus = 1
	order.PayMethod = payId
	order.TotalCount = totalCount
	order.TotalPrice = realyPrice
	order.TransitPrice = transferPrice
	//查询地址
	var addr models.Address
	addr.Id = addrid
	o.Read(&addr)

	order.Address = &addr

	//执行插入操作
	o.Insert(&order)


	//想订单商品表中插入数据
	conn,_ :=redis.Dial("tcp","192.168.110.81:6379")

	for _,skuid := range skuids{
		id,_ := strconv.Atoi(skuid)

		var goods models.GoodsSKU
		goods.Id = id
		i := 3

		for i> 0{
		o.Read(&goods)

		var orderGoods models.OrderGoods

		orderGoods.GoodsSKU = &goods
		orderGoods.OrderInfo = &order

		count ,_ :=redis.Int(conn.Do("hget","cart_"+strconv.Itoa(user.Id),id))

		if count > goods.Stock{
			resp["code"] = 2
			resp["errmsg"] = "商品库存不足"
			this.Data["json"] = resp
			o.Rollback()  //标识事务的回滚
			return
		}

		preCount := goods.Stock

		time.Sleep(time.Second * 5)
		beego.Info(preCount,user.Id)

		orderGoods.Count = count

		orderGoods.Price = count * goods.Price

		o.Insert(&orderGoods)

		goods.Stock -= count
		goods.Sales += count

		updateCount,_:=o.QueryTable("GoodsSKU").Filter("Id",goods.Id).Filter("Stock",preCount).Update(orm.Params{"Stock":goods.Stock,"Sales":goods.Sales})
		if updateCount == 0{
			if i >0 {
				i -= 1
				continue
			}
			resp["code"] = 3
			resp["errmsg"] = "商品库存改变,订单提交失败"
			this.Data["json"] = resp
			o.Rollback()  //标识事务的回滚
			return
		}else{
			conn.Do("hdel","cart_"+strconv.Itoa(user.Id),goods.Id)
			break
		}
		}

	}

	//返回数据
	o.Commit()  //提交事务
	resp["code"] = 5
	resp["errmsg"] = "ok"
	this.Data["json"] = resp

}

//处理支付
func(this*OrderController)HandlePay(){
	var aliPublicKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtPLfffeuLcVVBAZmiQuA7BtFGv7GKG6mWP7P+r9/koOTsICX6PObhGZwSR1BYtJhgcdimRI3UBBxyR3P4Ay7egpcconLuyxqZYNfohfVRL48MfIyS7cHDdNkjz2r70gOLfjYwchM6ttkzftME0k4QLJf/Y+qbSCiWvZ+9YRFmHo9Iq8juKDbnYkYmhoq7LDUxwVh7k9JeYW20kTIJecfNutCWGOcAC01jFymbNglrne8cUWet+qgY2WhGwEK1+2r1lWu+0azsNPPF3i3vVPAH1F2yxz6njhU26zO7A6+sB5Ff4DiULh3UAH9yID6LKJNBVJTpKobwidhFqk3ip5UqQIDAQAB" // 可选，支付宝提供给我们用于签名验证的公钥，通过支付宝管理后台获取



	var privateKey = "MIIEogIBAAKCAQEAuVkA0yyrqrWay/pjywY6ev6/+IvutzEuIckAQDXxVlU349ED"+
	"9Oqfi3gxbDYBmXg8bvUleSvuXFSuRagaEFp5XuvaS4Lh0D9ReWwbSgnAlChWFRde" +
	"o+zDl/nDmStmd5pmmlvJgi04+p2orNWBnsADYi25Jq1Hr006zz2k9GZzj8W6GMFx" +
	"URFKYrXPaexlkM/4oY3Bdnjf4dzyduLGGhLb7JhsCwHdKY7LGnqI/3ipK5HCvUoi" +
	"O/0QwN3SgV7u23DU8E+ewK0Yd6ncTB408Pw9kp5RXK3QBPN/P861+/aF9pZP7iZG" +
	"lZBAvq892VDkPTv7CxRPe3VDyYLiT31rqNgJ9wIDAQABAoIBAEp7WbmC2flfwTT3" +
	"CeMsPZPvi3V1xhgXy1IIU/F5u+HVnQAPnmdtYW/KfRHfIgVqK97q5IQtAzxXSzDy" +
	"vIaV1PAwFykBD31/9F2288Bs6tae3vjay01Ud8U6uT90EVk+0rx05iVJxvvvHzlV" +
	"EyGYR8PMW/sO5x+rCVb+jqsoAIhlXbDcocpyPZpdO07n6mIVhkFkLFElM+2awoAD" +
	"1fjtpSGUa/fAoUtBHxcrnSgS7byo4xsszX2RadtEQ4DDVw3NsS+9ClFLJAAa5HHC" +
	"Dj1Wq7jHxSKqMu9cEXBoZfZpTcOnQdlT9QGAwjuYOl0IA1Yq6jqP2myTSJspR/M0" +
	"oSqa5wECgYEA2ri2ZbVdiErlh1GiLpLVhND2mxsKGjJZr9+8GckUwZbSKj/qX5G4" +
	"LQaODwFQgZlc+mh0JYdi1XpXwXTfBuzmjS8ckHhTEdwl/4GZXjXTQlmoA5TW4UwR" +
	"ARZXNYUS6NKDMycLwLOXzMcYcrr+/ENqEakrlfTgndZ4aAhQttbVqBsCgYEA2PAe" +
	"JkpSUa3LGBL4eNbJOWyjxRmhHTiL0CwV+S3O109ODqlgCNTINVOWHbqASGRCyEq6" +
	"OAQ+ZcRdVH/V1liQguFpPM1GiIUhNpBJvOtTghIzmjMBVl3pBGrZRHEkRetCB08i" +
	"eJwVCLAZiPe5y6/1/YxFNzo9CQlDXvBsRmcLO1UCgYAbke8D8RGiLXazUPn1jvK1" +
	"NDXxpT3nwXMGtVgbk/o20NEbHEVp2I9ztYDQqWTBgVh0BBin5mHx8OMA8r9uOwxw" +
	"vYCHQXOPK8XqaCax9mzzzyNbmDZh3dnC3lMN3wFcMbTyDLjxiHHZETumsqWTnNfQ" +
	"9BcXZu+tVayFyI6MDZaPAwKBgAnCI1kNls5dxvj6QXsODlcq7+L52Cl8Va2zjfl7" +
	"egZtZtF6BPvgtnDPpb0ImqSm/eoMknvalQP25UvbxD60FSwN/7Hgef/CHVBbBTYe" +
	"T89WCkQxbyn3Z3fvZn6RqFQM1ReHE8HhI4EUitGCczUaYTJakPJ/CCfT5tfqcLRq" +
	"BjNRAoGAeO6/oTQaEaerBOM3HfJp1TV55As11xY+yZtzGhsGcywvBg7EZYWCm1z0" +
	"cw3oLsaVbFlXLLJjcjsLT63Llh3+Ua9m1J1XRPV2SIxCSRQHF1xeyaCE5d/VzOgF" +
	"GBNcPN71at6g+0S6p119eBNtVl6d43R+WjjPPfwheieeS1KyIec=" // 必须，上一步中使用 RSA签名验签工具 生成的私钥

	var appId = "2016092200569649"
	var client = alipay.New(appId, aliPublicKey, privateKey, false)

	//获取数据
	orderId := this.GetString("orderId")
	totalPrice := this.GetString("totalPrice")

	var p = alipay.AliPayTradePagePay{}
	p.NotifyURL = "http://xxx"
	p.ReturnURL = "http://192.168.110.81:8080/user/payok"
	p.Subject = "天天生鲜购物平台"
	p.OutTradeNo = orderId
	p.TotalAmount = totalPrice
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"

	var url, err = client.TradePagePay(p)
	if err != nil {
		fmt.Println(err)
	}

	var payURL = url.String()
	this.Redirect(payURL,302)
}

//支付成功
func(this*OrderController)PayOk(){
	//获取数据
	//out_trade_no=999998888777
	orderId := this.GetString("out_trade_no")


	//校验数据
	if orderId ==""{
		beego.Info("支付返回数据错误")
		this.Redirect("/user/userCenterOrder",302)
		return
	}

	//操作数据

	o := orm.NewOrm()
	count,_:=o.QueryTable("OrderInfo").Filter("OrderId",orderId).Update(orm.Params{"Orderstatus":2})
	if count == 0{
		beego.Info("更新数据失败")
		this.Redirect("/user/userCenterOrder",302)
		return
	}

	//返回视图
	this.Redirect("/user/userCenterOrder",302)
}
