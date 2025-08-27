from dis import code_info
import os
import json
import subprocess
import base64

prompt_init = """
你是代码安全专家tsj，专注于从代码中识别敏感信息泄露，你的输出应该是json格式
"""

prompt_user_template = """
【任务背景】
我将提供一个日志打印函数{log_func}的调用点代码, 这个函数的参数将会在日志中输出，请判断它是否打印了敏感信息（如密码、密钥、令牌等）。
对于每一个参数，你应该详细分析该参数的来源来判断，比如如果打印某个变量，分析该变量是否包含敏感信息。
【强制输出要求】
可以确定{log_func}的参数一定会被打印到日志里，你只需要分析{log_func}的参数是否包含敏感信息即可。
强制要求你不能对{log_func}使用get_symbol和find_refs功能。
【例子1】
如果该变量为结构体，应该使用查看定义功能get_symbol查看该结构体是否包含敏感信息成员。比如有代码：
```
struct task example;
print_task(example);
```
此时应该使用get_symbol功能获取task结构体的定义，比如获取到task结构体定义为
```
struct task{{
    char task_device_passwd[10];
    int task_id;
}}
```
那么虽然example看起来没有敏感信息，但其实多分析一点可以发现其实是有敏感信息passwd打印的。
【例子2】
如果有被打印的变量来自函数参数即上一层函数传递下来的，应该使用查找函数引用功能find_refs查看调用函数如何组装该变量并传递下来的。比如有代码：
```
void kill_task(char *task_id){{
    print_log("%s",task_id);
}}
```
此时应该使用find_refs功能获取kill_task的引用信息，查看caller函数如何调用kill_task函数，比如获取到的引用信息为
```
void task_manager(){{
    int id = 123;
    char task_id[100] = {{0}};
    char password[] = get_password_from_db();
    sprintf(task_id,"%s_%d",password,id);
    kill_task(task_id);
}}
```
那么虽然task_id看起来没有敏感信息，但其实多分析一点可以发现其实是有敏感信息password打印的。
【供参考的敏感信息pattern】
[
    "password",
    "passwd",
    "pswd",
    "secret",
    "token",
    "key",
    "证书",
    "私钥",
    "auth"
    "private_key",
    ]
【待分析的代码】
{content}
"""


if __name__ == "__main__":
    # os.system('bin/task_publisher find_refs print_log --code-server test_c_file')
    func_name = 'print_log'
    binary = 'bin/task_publisher'
    code_server = 'test_c_file'
    llm_config = 'qwen3-30b'
    cmd_args = [binary, 'find_refs', func_name, '--code-server', code_server]
    result = subprocess.run(cmd_args, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"Command failed with return code {result.returncode}")
        print(cmd_args)
        print(result.stderr)
        exit(1)
    print(result.stdout)
    # print(result.stderr)
    calls = json.loads(result.stdout)
    for call in calls['callers']:
        content = call
        prompt_user = prompt_user_template.format(content=content,log_func=func_name)
        prompt_init_b64 = base64.b64encode(prompt_init.encode('utf-8')).decode('ascii')
        prompt_user_b64 = base64.b64encode(prompt_user.encode('utf-8')).decode('ascii')
        cmd_args = [binary, 'submit', '--system-prompt-b64', prompt_init_b64, '--user-prompt-b64', prompt_user_b64, '--code-server', code_server, '--llm-config', llm_config, '--id', 'test_print_log']
        result = subprocess.run(cmd_args, capture_output=True, text=True)
        if result.returncode != 0:
            print(f"Command failed with return code {result.returncode}")
            print(cmd_args)
            print(result.stderr)
            print(result.stdout)
            exit(1)
        print(result.stdout)
