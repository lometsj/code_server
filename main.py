#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import os
import subprocess
import json
import re
import argparse
import traceback
from typing import List, Dict, Any, Tuple, Optional
import logging
from unittest import result
import openai
import time
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
    """代码分析器，负责调用cscope生成代码关系数据"""

    def __init__(self, code_dir: str, data_dir: str):
        self.code_dir = os.path.abspath(code_dir)
        self.data_dir = os.path.abspath(data_dir)
        
    def get_code_content(self, file, line, end):
        with open(os.path.join(self.code_dir,file),'rb') as f:
            code_bytes = f.read()
            encoding = detect(code_bytes)
            code_str = code_bytes.decode(encoding['encoding'])
            lines = code_str.split('\n')
            code= '\n'.join(lines[line-1:end])
            return code
    
    def get_ref_callee_content(self, file_path, line_num):
        # print(f'f:{file_path} l:{line_num}')
        res = subprocess.run(
            ['ctags','--fields=+ne-P','--output-format=json','-o','-',file_path],
            cwd=self.code_dir,
            capture_output=True,
            text=True,
            check=True
        )
        syms = res.stdout.strip().split('\n')
        for sym in syms:
            try:
                sym_dict = json.loads(sym)
                if sym_dict['kind'] != 'function':
                    continue
                if line_num > sym_dict['line'] and sym_dict['end'] > line_num:
                    return self.get_code_content(file_path, sym_dict['line'], sym_dict['end'])
            except:
                print(sym_dict)
                

    def get_symbol_info(self, symbol: str) -> Dict:
        """获取符号的信息,先用readtags 看在哪些文件里，再用ctags获取具体的"""
        ret = {}
        ret['status'] = 'failed'
        try:
            # 有时候大模型要求`struct xxx`的符号，直接查不行，要换成xxx来查
            if symbol.startswith('struct'):
                symbol = symbol.split(' ')[1]
            # 有时候是xx->yy,直接插也不行，直接查yy
            if '->' in symbol:
                symbol = symbol.split(' ')[1]
            # readtags -t .tsj/tags malloc
            result = subprocess.run(
                ["readtags", "-t", ".tsj/tags", symbol],
                cwd=self.code_dir,
                capture_output=True,
                text=True,
                check=True
            )
            
            lines = result.stdout.strip().split('\n')
            if not lines or lines[0] == '':
                return ret
            ret_list = []
            for line in lines:
                parts = line.split(maxsplit=4)
                file = parts[1]
                # ctags --fields=+ne-P --output-format=json -o - test.c
                res = subprocess.run(
                    ['ctags','--fields=+ne-P','--output-format=json','-o','-',file],
                    cwd=self.code_dir,
                    capture_output=True,
                    text=True,
                    check=True
                )
                syms = res.stdout.strip().split('\n')
                tmp_sym_to_find = symbol
                i = 0
                loop_count = 0
                max_loops = len(syms) * 2
                while i<len(syms) and loop_count < max_loops:
                    loop_count = loop_count + 1
                    sym_dict = json.loads(syms[i])
                    if sym_dict['name'] != tmp_sym_to_find:
                        i = i + 1
                        continue
                    # typeref定义的结构体，查符号名指向的是匿名结构体，没有end，要重新找该匿名结构体获取end。先假设符号和结构体在同一个文件。
                    if 'end' not in sym_dict and 'typeref' in sym_dict:
                        tmp_sym_to_find = sym_dict['typeref'].split(':')[1]
                        i = 0
                        continue
                    sym_dict['content'] = self.get_code_content(file, sym_dict['line'], sym_dict['end'])
                    ret_list.append(sym_dict)
                    break
            ret['res_list'] = ret_list
            ret['status'] ='success'
            return ret
        except Exception as e:
            logger.error(f"获取符号信息时出错: {str(e)}")
            logger.error(traceback.print_exc())
            return ret
    
    def find_all_refs(self, symbol: str) -> List[Dict]:
        """获取调用这个符号的caller的代码上下文"""
        try:
            result = subprocess.run(
                ["global", "-xsr",symbol],
                cwd=self.code_dir,
                capture_output=True,
                text=True,
                check=True
            )

            lines = result.stdout.strip().split('\n')
            # print(lines)
            if not lines or lines[0] == '':
                return []
            
            callers_content = []
            for line in lines:
                if not line.strip():
                    continue
                
                parts = line.split(maxsplit=3)
                if len(parts) < 3:
                    continue
                
                callee, file_path, line_num, call_line =parts[0], parts[2], parts[1], parts[3]
                # file_path, calle, line_num, call_line = parts[0], parts[1], parts[2], parts[3]
                # logger.info(callee, file_path, line_num,call_line)
                # print(f'file={file_path} line_num={line_num}')
                caller_content = self.get_ref_callee_content(file_path, int(line_num))
                # print(caller_content)
                callers_content.append(caller_content)
                # refs.append({
                #     "callee": callee,
                #     "file": file_path,
                #     "line": int(line_num),
                #     "call_line": call_line,
                #     "caller_content": self.get_ref_callee_content(file_path, int(line_num))
                # })
            ret_content = list(dict.fromkeys(callers_content))
            ret_content = [item for item in ret_content if item is not None]

            return ret_content
            
        except Exception as e:
            logger.error(f"获取调用信息时出错: {str(e)}")
            logger.error(f"错误信息: {traceback.format_exc()}")
            return [] 

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
        # yes,indeed
        self.prompt_need = '''
【代码分析功能说明】
你可以使用get_symbol功能获取符号定义信息，可以使用find_refs获取函数引用信息以便于向上追踪函数调用栈。
具体请求格式见下面 `输出结果要求`。
【强制输出结果要求】
必须在回答中包含带tsj的标签，以下标签三选一[tsj_have][tsj_nothave][tsj_next]:
- 如判断有代码问题: [tsj_have] 并提供 {"problem_type": "问题类型", "context": "代码上下文"}
- 如判断无代码信息: [tsj_nothave]
- 如果不能判断，需要获取信息进一步分析，请包含[tsj_next]，并包含get_symbol或者find_refs请求获取更多代码信息,详细格式如下：
1. 如果需要知道某个函数，宏或者变量的定义，使用get_symbol获取符号信息: {"command": "get_symbol", "sym_name": "符号名称"}
2. 如果需要进一步分析数据流，使用find_refs获取调用信息: {"command": "find_refs", "sym_name\": "符号名称"}

【JSON格式返回要求】
请以JSON格式返回你的回答，例如：
{"tag": "tsj_have", "problem_info": {"problem_type": "问题类型", "context": "代码上下文"}, "response": "你的分析和解释"}
或
{"tag": "tsj_nothave", "response": "你的分析和解释"}
或
{"tag": "tsj_next", "requests": [{"command": "get_symbol", "sym_name": "符号名称"}], "response": "你的分析和解释"}
'''
    
    def process_llm_request(self, request: Dict) -> Dict:
        """处理LLM发出的信息请求"""
        logger.info("处理LLM发出的信息请求")
        if "command" not in request:
            return {"error": "缺少command字段"}
        
        if request["command"] == "get_symbol" and "sym_name" in request:
            # 获取符号信息
            result = self.code_analyzer.get_symbol_info(request["sym_name"])
            
            # 如果HTTP服务开启且没有找到结果，等待用户输入
            if self.http_server and (not result or result.get("status") == "failed" or 
                                    (isinstance(result, list) and len(result) == 0)):
                logger.info(f"未找到符号 {request['sym_name']} 的信息，等待用户输入")
                
                # 通过HTTP服务等待用户输入
                user_input = self.http_server.wait_for_user_input("get_symbol", request["sym_name"])
                
                # 尝试解析用户输入为JSON
                try:
                    user_result = json.loads(user_input)
                    result = user_result
                except json.JSONDecodeError:
                    # 如果不是JSON，将用户输入作为文本内容
                    result = {"type": "user_input", "definition": user_input}
            
            return {
                "command": "get_symbol",
                "sym_name": request["sym_name"],
                "result": result
            }
        elif request["command"] == "find_refs" and "sym_name" in request:
            # 获取引用信息
            result = self.code_analyzer.find_all_refs(request["sym_name"])
            
            # 如果HTTP服务开启且没有找到结果，等待用户输入
            if self.http_server and (not result or len(result) == 0):
                logger.info(f"未找到符号 {request['sym_name']} 的引用信息，等待用户输入")
                
                # 通过HTTP服务等待用户输入
                user_input = self.http_server.wait_for_user_input("find_refs", request["sym_name"])
                
                # 尝试解析用户输入为JSON
                try:
                    user_result = json.loads(user_input)
                    result = user_result
                except json.JSONDecodeError:
                    # 如果不是JSON，将用户输入作为文本内容
                    result = [user_input]
            
            return {
                "command": "find_refs",
                "sym_name": request["sym_name"],
                "result": result
            }
        else:
            return {"error": "未知命令或缺少必要参数"}
    
    def extract_requests(self, llm_response: str) -> List[Dict]:
        """从LLM响应中提取JSON请求"""
        requests = []
        
        # 先移除<think></think>标签中的内容
        think_pattern = r'<think>.*?</think>'
        cleaned_response = re.sub(think_pattern, '', llm_response, flags=re.DOTALL)
        
        # 尝试解析整个响应作为JSON
        try:
            # 如果整个响应是一个有效的JSON对象
            full_response = json.loads(cleaned_response)
            if isinstance(full_response, dict):
                # 如果JSON对象包含command字段，直接添加
                if "command" in full_response:
                    requests.append(full_response)
                # 如果JSON对象包含requests字段且为列表，添加所有请求
                elif "requests" in full_response and isinstance(full_response["requests"], list):
                    for req in full_response["requests"]:
                        if isinstance(req, dict) and "command" in req:
                            requests.append(req)
                # 检查是否有其他可能的命令字段
                for key, value in full_response.items():
                    if isinstance(value, dict) and "command" in value:
                        requests.append(value)
        except json.JSONDecodeError:
            # 如果整个响应不是有效的JSON，则回退到原来的方法查找JSON对象
            json_pattern = r'\{.*?\}'
            for match in re.finditer(json_pattern, cleaned_response, re.DOTALL):
                try:
                    json_str = match.group(0)
                    req = json.loads(json_str)
                    if isinstance(req, dict) and "command" in req:
                        requests.append(req)
                except json.JSONDecodeError:
                    continue
        
        return requests
    
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
            
            # 检查系统消息中是否包含JSON相关指令，如果没有则添加
            has_json_instruction = False
            for msg in messages:
                if msg["role"] == "system" and "json" in msg["content"].lower():
                    has_json_instruction = True
                    break
            
            # 如果没有JSON指令，修改系统消息或添加JSON指令
            if not has_json_instruction and len(messages) > 0 and messages[0]["role"] == "system":
                messages[0]["content"] += "\n请以JSON格式返回你的回答。"
            
            for attempt in range(max_retries):
                try:
                    if stream and self.http_server:
                        # 使用流式接口
                        response = self.client.chat.completions.create(
                            model=self.model,
                            messages=messages,
                            temperature=0.1,  # 低温度以获得更确定的回答
                            max_tokens=2000,  # 设置最大token数
                            top_p=0.95,  # 设置top_p参数
                            frequency_penalty=0,  # 设置frequency_penalty参数
                            presence_penalty=0,  # 设置presence_penalty参数
                            response_format={"type": "json_object"},  # 设置返回格式为JSON
                            stream=True  # 启用流式输出
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
                        response = self.client.chat.completions.create(
                            model=self.model,
                            messages=messages,
                            temperature=0.1,  # 低温度以获得更确定的回答
                            max_tokens=2000,  # 设置最大token数
                            top_p=0.95,  # 设置top_p参数
                            frequency_penalty=0,  # 设置frequency_penalty参数
                            presence_penalty=0,  # 设置presence_penalty参数
                            response_format={"type": "json_object"}  # 设置返回格式为JSON
                        )
                        return response.choices[0].message.content
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
            {"role": "system", "content": problem_prompt['system'] + "\n请以JSON格式返回你的回答，包含必要的标签和问题信息。"},
            {"role": "user", "content": problem_prompt['init_user']+self.prompt_need}
        ]
        conversation_complete = False
        max_turns = 5  # 限制对话轮数
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
                
            #去除回答里的think
            llm_response = re.sub(r'<think>.*?</think>', '', llm_response, flags=re.DOTALL)
            messages.append({"role":"assistant", "content": llm_response})
            
            # 如果HTTP服务开启且使用非流式接口，添加完整助手消息
            # 流式接口已经在query_openai中实时添加消息块了
            if self.http_server and not self.http_server.has_active_stream:
                self.http_server.add_message("assistant", llm_response)
            
            # 尝试解析整个响应为JSON
            try:
                response_json = json.loads(llm_response)
                
                # 检查JSON响应中是否包含标签信息
                if isinstance(response_json, dict):
                    # 检查是否有直接的标签字段
                    if "tag" in response_json:
                        tag = response_json["tag"]
                        if tag == "tsj_have" or tag == "tsj_nothave":
                            conversation_complete = True
                            if tag == "tsj_have":
                                result["has_problem_info"] = True
                                if "problem_info" in response_json and isinstance(response_json["problem_info"], dict):
                                    if "problem_type" in response_json["problem_info"] and "context" in response_json["problem_info"]:
                                        result["problem_info"] = response_json["problem_info"]
            except json.JSONDecodeError:
                # 如果不是有效的JSON，回退到原来的文本处理方式
                if '[tsj_have]' in llm_response or '[tsj_nothave]' in llm_response:
                    conversation_complete = True
                    if '[tsj_have]' in llm_response:
                        result["has_problem_info"] = True
                    try:
                        json_pattern = r'\{.*?\}'
                        for match in re.finditer(json_pattern, llm_response, re.DOTALL):
                            json_str = match.group(0)
                            problem_info = json.loads(json_str)
                            if "problem_type" in problem_info and "context" in problem_info:
                                result["problem_info"] = problem_info
                                break
                    except Exception as e:
                        logger.warning(f"提取标签出错: {str(e)}")
            
            # 如果对话未完成，处理请求
            if not conversation_complete:
                requests = self.extract_requests(llm_response)
                if len(requests) != 0:
                    # 处理所有请求
                    responses = [self.process_llm_request(req) for req in requests]
                    response_message = "【代码分析系统回答】:\n\n" + json.dumps(responses, ensure_ascii=False, indent=2)
                    response_message = response_message + '''
【强制输出结果要求】
你必须只分析对话开始给出的问题，不能分析其他类型的问题
必须在回答中包含带tsj的标签，以下标签三选一[tsj_have][tsj_nothave][tsj_next]:
- 如判断有代码问题: [tsj_have] 并提供 {"problem_type": "问题类型", "context": "代码上下文"}
- 如判断无代码信息: [tsj_nothave]
- 如果不能判断，需要获取信息进一步分析，请包含[tsj_next]，并包含get_symbol或者find_refs请求获取更多代码信息,详细格式如下：
1. 如果需要知道某个函数，宏或者变量的定义，使用get_symbol获取符号信息: {"command": "get_symbol", "sym_name": "符号名称"}
2. 如果需要进一步分析数据流，使用find_refs获取调用信息: {"command": "find_refs", "sym_name\": "符号名称"}

请以JSON格式返回你的回答，例如：
{"tag": "tsj_have", "problem_info": {"problem_type": "问题类型", "context": "代码上下文"}, "response": "你的分析和解释"}
或
{"tag": "tsj_nothave", "response": "你的分析和解释"}
或
{"tag": "tsj_next", "requests": [{"command": "get_symbol", "sym_name": "符号名称"}], "response": "你的分析和解释"}
'''
                    messages.append({"role": "user", "content": response_message})
                    logger.info(f"用户请求: {response_message}")
                    
                    # 如果HTTP服务开启，添加系统消息
                    if self.http_server:
                        self.http_server.add_message("user", response_message)
                else:
                    prompt = "请基于已有信息给出最终结论，是否包含敏感信息。记得包含[tsj_have]或[tsj_nothave]或[tsj_next]标记。请以JSON格式返回你的回答。"
                    messages.append({"role": "user", "content": prompt})
                    
                    # 如果HTTP服务开启，添加系统消息
                    if self.http_server:
                        self.http_server.add_message("user", prompt)
            turn += 1
        if turn == max_turns:
            result['has_problem_info'] = True
            result['problem_info'] = '对话轮数耗尽仍没有问答，建议重点审视。'

        result['conversation'] = messages
        
        # 如果HTTP服务开启，更新任务状态为空闲
        if self.http_server:
            self.http_server.update_task_status("idle")
        
        return result


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
            json.dump(results, f, ensure_ascii=False, indent=2)
    
    

def main():
    parser = argparse.ArgumentParser(description='敏感信息日志打印分析工具')
    parser.add_argument('--code-dir', required=True, help='要分析的代码目录')
    parser.add_argument('--data-dir', default='./tsj_data', help='数据和报告输出目录')
    parser.add_argument('--config', default='./config.json', help='配置文件路径')
    parser.add_argument('--interactive', default=False, help='要不要在代码分析给不出回答的时候手工介入给大模型返回答案')
    parser.add_argument('--http', action='store_true', help='开启HTTP服务，可以通过Web界面查看对话流和手动输入符号信息')
    parser.add_argument('--port', type=int, default=8080, help='HTTP服务端口号')

    
    args = parser.parse_args()
    
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
    code_analyzer = CodeAnalyzer(args.code_dir, args.data_dir)
    
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
