#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import os
import subprocess
import json
import re
import argparse
import traceback
from typing import List, Dict, Any, Literal, Tuple, Optional
import logging
from unittest import result
import openai
import time
import requests  # 添加导入
from sensetive import sensitive_problem
from overflow import overflow_problem
from command_inject import command_inject_problem
from mem_leak import mem_leak_problem
from charset_normalizer import detect
from jsoncpp import jsoncpp_problem




# 配置日志
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger(__name__)

class CodeAnalyzer:
    """代码分析器，通过调用code_server获取代码内容"""

    def __init__(self, server):
        #server的格式是ip:port，分割后保存到self.server_ip和self.server_port
        self.server_ip, self.server_port = server.split(':')
        self.server_port = int(self.server_port)
        self.server_url = f'http://{self.server_ip}:{self.server_port}'
        
    def get_symbol_info(self, symbol: str) -> Dict:
        """发送post请求到/api/get_symbol 参数为{"symbol":symbol}"""
        url = f'{self.server_url}/api/get_symbol'
        params = {'symbol': symbol}
        response = requests.post(url, json=params)
        return response.json()
    
    def find_all_refs(self, symbol: str) -> List[Dict]:
        """发送post请求到/api/find_refs 参数为{"symbol":symbol}"""
        url = f'{self.server_url}/api/find_refs'
        params = {'symbol': symbol}
        response = requests.post(url, json=params)
        res_json = response.json()
        if 'error' in res_json:
            print('find refs error:'+res_json['error'])
            return []
        return res_json['callers']

class LLMAnalyzer:
    """LLM分析器，负责与大模型交互分析日志函数是否打印敏感信息"""
    
    def __init__(self, code_analyzer: CodeAnalyzer, api_key: str, base_url: str, model: str,
                 http_server=None):
        self.code_analyzer = code_analyzer
        self.api_key = api_key
        self.base_url = base_url
        self.model = model
        self.client = openai.OpenAI(api_key=api_key, base_url=base_url)
        self.http_server = http_server
        
        if api_key:
            openai.api_key = api_key
        
        # 简化提示词，移除JSON格式要求
        self.prompt_need = '''
【代码分析功能说明】
你可以使用get_symbol功能获取符号定义信息，可以使用find_refs获取函数引用信息以便于向上追踪函数调用栈。

【强制输出结果要求】
必须在回答中tag字段，值为[tsj_have][tsj_nothave][tsj_next]:
- 如判断有代码问题: [tsj_have] 并提供 {"problem_type": "问题类型", "context": "代码上下文"}
- 如判断无代码问题: [tsj_nothave]
- 如果不能判断，需要获取信息进一步分析，请包含[tsj_next]，并包含get_symbol或者find_refs请求获取更多代码信息,详细格式如下：
1. 如果需要知道某个函数，宏或者变量的定义，使用get_symbol获取符号信息: {"command": "get_symbol", "sym_name": "符号名称"}
2. 如果需要进一步分析数据流，使用find_refs获取调用信息: {"command": "find_refs", "sym_name\": "符号名称"}

【输出要求】
【JSON格式返回要求】
请以JSON格式返回你的回答，例如：
{"tag": "tsj_have", "problem_info": {"problem_type": "问题类型", "context": "代码上下文"}, "response": "你的分析和解释"}
或
{"tag": "tsj_nothave", "response": "你的分析和解释"}
或
{"tag": "tsj_next", "requests": [{"command": "get_symbol", "sym_name": "符号名称"}], "response": "你的分析和解释"}
或
{"tag": "tsj_next", "requests": [{"command": "find_refs", "sym_name": "符号名称"}], "response": "你的分析和解释"}
或
{"tag": "tsj_next", "requests": [{"command": "get_symbol", "sym_name": "符号名称"},{"command": "find_refs", "sym_name": "符号名称"}], "response": "你的分析和解释"}
'''
        

    def query_openai(self, messages: List[Dict], stream=False) -> str:
        """调用OpenAI API进行查询
        
        Args:
            messages: 消息列表
            stream: 是否使用流式接口
            
        Returns:
            如果stream=False，返回完整的响应文本
            如果stream=True，返回完整的响应文本（同时会通过HTTP服务器实时推送）
        """
        try:
            # 添加重试机制
            max_retries = 3
            retry_delay = 2
            
            for attempt in range(max_retries):
                try:
                    if stream and self.http_server:
                        # 使用流式接口
                        response = self.client.chat.completions.create(
                            model=self.model,
                            messages=messages,
                            temperature=0.1,
                            max_tokens=2000,
                            top_p=0.95,
                            frequency_penalty=0,
                            presence_penalty=0,
                            tools=self.functions,  # 添加tools参数
                            tool_choice="auto",  # 设置tool_choice
                            stream=True
                        )
                        
                        # 收集完整响应用于返回
                        full_response = ""
                        
                        # 开始流式消息
                        self.http_server.add_message_chunk("assistant", "")
                        
                        # 处理流式响应
                        for chunk in response:
                            if chunk.choices and len(chunk.choices) > 0:
                                delta = chunk.choices[0].delta
                                if hasattr(delta, 'content') and delta.content:
                                    # 将每个块添加到完整响应
                                    full_response += delta.content
                                    
                                    # 实时推送到HTTP服务器
                                    self.http_server.add_message_chunk("assistant", delta.content)
                                
                                
                        
                        # 流式响应完成，结束消息流
                        self.http_server.finish_stream_message()
                        
                        # 返回完整响应
                        return full_response
                    else:
                        # 使用非流式接口
                        response = self.client.beta.chat.completions.parse(
                            model=self.model,
                            messages=messages,
                            temperature=0.1,
                            max_tokens=2000,
                            top_p=0.95,
                            frequency_penalty=0,
                            presence_penalty=0,
                            # tools=self.functions,  # 添加tools参数
                            tool_choice="auto",  # 设置tool_choice
                            response_format={"type": "json_object"}  # 设置返回格式为JSON
                        )
                        return response.choices[0].message
                except (openai.RateLimitError, openai.APIError) as e:
                    if attempt < max_retries - 1:
                        logger.warning(f"API调用失败，尝试重试 ({attempt+1}/{max_retries}): {str(e)}")
                        time.sleep(retry_delay * (2 ** attempt))  # 指数退避
                    else:
                        raise
        except Exception as e:
            logger.error(f"调用OpenAI API时出错: {str(e)}")
            logger.error(f"错误信息: {traceback.format_exc()}")
            return f"API调用错误: {str(e)}"
    
    def analyze_task(self, problem_prompt):
        messages = [
            {"role": "system", "content": problem_prompt['system'] + "\n请使用工具调用获取代码信息并分析问题。"},
            {"role": "user", "content": problem_prompt['init_user']+self.prompt_need}
        ]
        conversation_complete = False
        max_turns = 5
        turn = 0
        
        result = {
            "has_problem_info": False,
            "problem_info": None,
            "conversation": []
        }
        
        # 如果HTTP服务开启，更新任务状态为运行中
        if self.http_server:
            self.http_server.update_task_status("running")
            # 添加初始消息到HTTP服务
            for msg in messages:
                self.http_server.add_message(msg["role"], msg["content"])
        
        while not conversation_complete and turn < max_turns:
            # 调用OpenAI API获取响应
            if self.http_server:
                # 使用流式接口，响应会实时推送到HTTP服务器
                llm_response = self.query_openai(messages, stream=True)
            else:
                # 使用非流式接口
                llm_response = self.query_openai(messages, stream=False)
            
            
                # 处理普通响应
                response_content = llm_response.content if hasattr(llm_response, 'content') else str(llm_response)
                messages.append({"role": "assistant", "content": response_content})
                message = json.loads(response_content)
                print(message)
                # 如果HTTP服务开启且使用非流式接口，添加完整助手消息
                # 流式接口已经在query_openai中实时添加消息块了
                if self.http_server and not self.http_server.has_active_stream:
                    self.http_server.add_message("assistant", response_content)
                
                # 检查是否包含问题信息,通过tag判断，如果是tsj_have或者tsj_nothave就结束对话并将结果保存
                if 'tag' in message:
                    if message['tag'] == 'tsj_have' or message['tag'] == 'tsj_nothave':
                        conversation_complete = True
                        result['has_problem_info'] = message['tag'] == 'tsj_have'
                        result['problem_info'] = message['problem_info'] if 'problem_info' in message else None
                        result['response'] = message['response'] if 'response' in message else None
                    if message['tag'] == 'tsj_next':
                        # 处理tsj_next标签，添加请求到消息列表
                        for req in message['requests']:
                            if req['command'] == 'get_symbol':
                                messages.append({"role": "user", "content": str(self.code_analyzer.get_symbol_info(req['sym_name']))})
                            elif req['command'] == 'find_refs':
                                messages.append({"role": "user", "content": str(self.code_analyzer.find_all_refs(req['sym_name']))})
                
                
                
            turn += 1
        
        if turn == max_turns and not conversation_complete:
            result['has_problem_info'] = True
            result['problem_info'] = '对话轮数耗尽仍没有问答，建议重点审视。'

        result['conversation'] = messages
        
        # 如果HTTP服务开启，更新任务状态为空闲
        if self.http_server:
            self.http_server.update_task_status("idle")
        
        return result

# 定义自定义序列化方法
def custom_serializer(obj):
    """自定义JSON序列化器，处理OpenAI对象类型"""
    from openai.types.chat import ChatCompletionMessage, ChatCompletionMessageToolCall
    
    if isinstance(obj, ChatCompletionMessage):
        return {
            "content": obj.content,
            "role": obj.role,
            "tool_calls": obj.tool_calls if hasattr(obj, 'tool_calls') else None,
            "function_call": obj.function_call if hasattr(obj, 'function_call') else None
        }
    
    if isinstance(obj, ChatCompletionMessageToolCall):
        return {
            "id": obj.id,
            "type": obj.type,
            "function": {
                "name": obj.function.name,
                "arguments": obj.function.arguments
            }
        }
    
    # 处理其他可能的OpenAI对象
    if hasattr(obj, '__dict__'):
        return obj.__dict__
    
    if hasattr(obj, '__str__'):
        return str(obj)
    
    raise TypeError(f"Object of type {obj.__class__.__name__} is not JSON serializable")

class ResultProcessor:
    """结果处理器，负责生成结果数据和HTML报告"""
    
    def __init__(self, data_dir: str):
        self.data_dir = data_dir
        timestamp = time.strftime("%Y%m%d%H%M%S")
        self.result_file = os.path.join(data_dir, f"analysis_result_{timestamp}.json")
        os.makedirs(data_dir, exist_ok=True)
        with open(self.result_file, 'w', encoding='utf-8') as f:
            json.dump([], f, ensure_ascii=False, indent=2)
        
    
    def save_results(self, result) -> str:
        """保存分析结果到JSON文件"""
        with open(self.result_file, 'r+', encoding='utf-8') as f:
            results = json.load(f)
            results.append(result)
            f.seek(0)
            json.dump(results, f, ensure_ascii=False, indent=2, default=custom_serializer)

def check_code_server(server: str) -> bool:
    """检查code_server是否存活"""
    try:
        ip, port = server.split(':')
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.settimeout(1)
        result = sock.connect_ex((ip, int(port)))
        sock.close()
        return result == 0
    except:
        return False
    
    

def main():
    parser = argparse.ArgumentParser(description='敏感信息日志打印分析工具')
    parser.add_argument('--server', required=True, help='code_server 的ip和port，格式为ip:port')
    parser.add_argument('--data-dir', default='./tsj_data', help='数据和报告输出目录')
    parser.add_argument('--config', default='./config.json', help='配置文件路径')
    parser.add_argument('--interactive', default=False, help='要不要在代码分析给不出回答的时候手工介入给大模型返回答案')
    parser.add_argument('--http', action='store_true', help='开启HTTP服务，可以通过Web界面查看对话流和手动输入符号信息')
    parser.add_argument('--port', type=int, default=8080, help='HTTP服务端口号')

    
    args = parser.parse_args()
    #检测code_server是否存活
    # if not check_code_server(args.server):
    #     logger.error(f"code_server {args.server} 未启动")
    #     return
    # 确保输出目录存在
    os.makedirs(args.data_dir, exist_ok=True)
    
    # 加载配置
    if not os.path.exists(args.config):
        # 创建默认配置
        default_config = {
            "log_functions": ["printf", "fprintf", "log_info", "log_error", "printk"],
            "max_call_depth": 3
        }
        with open(args.config, 'w', encoding='utf-8') as f:
            json.dump(default_config, f, indent=2)
        
        logger.info(f"已创建默认配置文件: {args.config}")
    
    with open(args.config, 'r', encoding='utf-8') as f:
        config = json.load(f)
    
    # 初始化代码分析器
    code_analyzer = CodeAnalyzer(args.server)
    
    # 获取日志函数调用路径
    log_functions = config.get("log_functions", [])
    max_depth = config.get("max_call_depth", 3)
    api_key = config.get("api_key",'')
    base_url = config.get("base_url",'')
    model = config.get("model", '')
    
    # 初始化HTTP服务器（如果启用）
    http_server = None
    if args.http:
        try:
            # 导入HTTP服务器模块
            from http_server import HTTPServer
            
            # 创建HTTP服务器实例
            http_server = HTTPServer(port=args.port)
            
            # 启动HTTP服务器
            http_server.start()
            
            # 确保templates目录存在
            os.makedirs("templates", exist_ok=True)
            
            # 创建Flask应用的模板目录链接
            if not os.path.exists(os.path.join("templates", "stream.html")):
                logger.info("创建模板链接")
                # 如果在Windows上，可能需要复制文件而不是创建符号链接
                if os.name == 'nt':
                    import shutil
                    shutil.copy2("templates/stream.html", "templates/stream.html")
                else:
                    # 在Linux/Unix上创建符号链接
                    if os.path.exists("templates/stream.html"):
                        os.remove("templates/stream.html")
                    os.symlink(os.path.abspath("templates/stream.html"), "templates/stream.html")
            
            logger.info(f"HTTP服务已启动，请访问 http://localhost:{args.port} 查看对话流")
        except ImportError as e:
            logger.error(f"无法导入HTTP服务器模块: {str(e)}")
            logger.error("请确保已安装Flask和Flask-CORS: pip install flask flask-cors")
            http_server = None
        except Exception as e:
            logger.error(f"启动HTTP服务器时出错: {str(e)}")
            logger.error(traceback.format_exc())
            http_server = None
    
    # 初始化LLM分析器
    llm_analyzer = LLMAnalyzer(code_analyzer, api_key, base_url, model, http_server)
    
    #################################register different type of vuln
    problem_type = [
        sensitive_problem,
        # command_inject_problem,
        # overflow_problem,
        # mem_leak_problem
        # jsoncpp_problem
    ]
    result_processor = ResultProcessor(args.data_dir)
    for problem in problem_type:
        task_list = problem.get_task_list(config, code_analyzer)
        # print(task_list)
        #todo batch mode
        for i in range(len(task_list)):
            task = task_list[i]
            result = llm_analyzer.analyze_task(problem.prepare_context(task))
            print('one task complete,res:',result)
            result_processor.save_results(result)
    
    logger.info(f"分析完成！结果已保存到: {result_processor.result_file}")


if __name__ == "__main__":
    main()
