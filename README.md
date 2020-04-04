
# edit by zhuxiujia@qq.com
更改
* 原项目由GoPkg 迁移到go.mod 包管理 兼容最新的go语言规范（原项目因为历史原因使用GoPkg）
* 抽象迁移出core包.以解决无法debug的问题。抽象出route 接口，client接口（原项目因为历史原因使用struct结构体 而不是interface接口）
* core包属性均改为首字母大写，方便异包调用
* 免除make 操作，im,imr,ims 均可支持debug调试和go run 命令
* 分包更明确，可兼容docker file容器打包，调试，部署、方便集成于k8s等容器环境

# im service
1. 支持点对点消息, 群组消息, 聊天室消息
2. 支持集群部署
3. 单机支持50w用户在线
4. 单机处理消息5000条/s
5. 支持超大群组(3000人)

*服务器硬件指标：32g 16核*



## docker 部署数据库
```shell script
docker run -p 6379:6379 --name redis -d redis redis-server --appendonly yes
docker run -p 3306:3306 --name mysql5.7 -e MYSQL_ROOT_PASSWORD=123456 -d mysql:5.7
```



## 编译运行

1. 安装go编译环境

   参考链接:https://golang.org/doc/install

2. 使用intellij系列的ide产品GoLand下载检出im_service代码

   cd $GOPATH/src/github.com/GoBelieveIO

   git clone https://github.com/GoBelieveIO/im_service.git

3. 安装依赖

   cd im_service

   go mod download



4. 安装mysql数据库, redis, 并导入db.sql

5. 配置程序
   配置项的说明参考ims.cfg.sample, imr.cfg.sample, im.cfg.sample


6. 启动程序

  * 创建配置文件中配置的im&ims消息存放路径

    mkdir /tmp/im

    mkdir /tmp/impending

  * 创建日志文件路径
    
    mkdir /data/logs/ims

    mkdir /data/logs/imr

    mkdir /data/logs/im

  * 启动im服务
   运行debug (GoLand 执行的话，需要点debug按钮左边的edit配置，program arg 那栏添加参数 例如 -log_dir=xxx   imr.cfg.sample)
   或者使用go命令运行
   ```shell script
   go run imr.go -log_dir=/data/logs/imr   imr.cfg.sample
   go run ims.go -log_dir=/data/logs/ims   ims.cfg.sample
   go run im.go  -log_dir=/data/logs/im   im.cfg.sample 
   ```
   


## token的格式

    连接im服务器token存储在redis的hash对象中,脱离API服务器测试时，可以手工生成。
    $token就是客户端需要获得的, 用来连接im服务器的认证信息。
    key:access_token_$token
    field:
        user_id:用户id
        app_id:应用id


## 官方QQ群


## 官方网站
   https://developer.gobelieve.io/

## 相关产品
   https://goubuli.mobi/
