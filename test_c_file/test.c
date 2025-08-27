#include <stdio.h>
#include <string.h>
#include <time.h>
#include <stdlib.h>

#define LOG_FILE "password_log.txt"
#define MAX_PASSWORD_LENGTH 100

// 函数声明
void print_log(const char *message);
void process_password(const char *password);
void validate_input(const char *input);
void perform_extra_operations(int value);
int calculate_checksum(const char *data);
void handle_special_case(int condition);
void log_system_info();
void generate_report();
void initialize_system();
void cleanup_resources();

struct task{
	char task_passwd[32];
	int task_id
};


int main() {
    initialize_system();
    
    char password[MAX_PASSWORD_LENGTH];
    printf("请输入密码: ");
    fgets(password, MAX_PASSWORD_LENGTH, stdin);
    password[strcspn(password, "\n")] = '\0'; // 去除换行符
    
    validate_input(password);
    process_password(password);
    
    generate_report();
    cleanup_resources();
    
    return 0;
}

void initialize_system() {
    // 初始化系统资源
    printf("系统初始化中...\n");
    // 模拟初始化过程
    for (int i = 0; i < 5; i++) {
        perform_extra_operations(i);
    }
    log_system_info();
}

void test_mem_leak(){
    char *buf = malloc(1024);
    int condition = get_sth();
    if(condition !=0){
        return;
    }
    free(buf);
}

void log_system_info() {
    // 记录系统信息到日志
    time_t now = time(NULL);
    struct tm *local_time = localtime(&now);
    char time_str[100];
    strftime(time_str, sizeof(time_str), "%Y-%m-%d %H:%M:%S", local_time);
    
    char system_info[256];
    sprintf(system_info, "系统信息记录时间: %s", time_str);
    print_log(system_info);
}

void validate_input(const char *input) {
    // 验证输入是否有效
    if (input == NULL || strlen(input) == 0) {
        print_log("错误: 输入为空");
        exit(1);
    }
    
    // 检查特殊字符
    for (int i = 0; i < strlen(input); i++) {
        if (input[i] == ';' || input[i] == '|' || input[i] == '&') {
            print_log("警告: 输入包含特殊字符");
            break;
        }
    }
}

void process_password(const char *password) {
    // 处理密码逻辑
    printf("处理密码...\n");
    
    // 检查密码强度
    int has_upper = 0, has_lower = 0, has_digit = 0, has_special = 0;
    for (int i = 0; i < strlen(password); i++) {
        if (isupper(password[i])) has_upper = 1;
        else if (islower(password[i])) has_lower = 1;
        else if (isdigit(password[i])) has_digit = 1;
        else has_special = 1;
    }
    
    int strength = has_upper + has_lower + has_digit + has_special;
    char strength_str[50];
    switch (strength) {
        case 1:
            sprintf(strength_str, "密码强度: 弱");
            break;
        case 2:
            sprintf(strength_str, "密码强度: 中");
            break;
        case 3:
            sprintf(strength_str, "密码强度: 强");
            break;
        case 4:
            sprintf(strength_str, "密码强度: 非常强");
            break;
        default:
            sprintf(strength_str, "密码强度: 未知");
    }
    print_log(strength_str);
    
    // 计算校验和
    int checksum = calculate_checksum(password);
    char checksum_str[50];
    sprintf(checksum_str, "密码校验和: %d", checksum);
    print_log(checksum_str);
    
    // 特殊情况处理
    handle_special_case(strength >= 3);
    
    // 最终记录密码
    char log_message[200];
    sprintf(log_message, "记录密码: %s", password);
    print_log(log_message);
}

void handle_special_case(int condition) {
    // 处理特殊情况
    if (condition) {
        printf("执行特殊情况处理...\n");
        for (int i = 0; i < 3; i++) {
            perform_extra_operations(i * 2);
        }
    } else {
        perform_extra_operations(42);
    }
}

int calculate_checksum(const char *data) {
    // 计算简单校验和
    int checksum = 0;
    for (int i = 0; i < strlen(data); i++) {
        checksum += data[i];
    }
    return checksum % 100;
}

void perform_extra_operations(int value) {
    // 执行额外操作
    printf("执行额外操作，值: %d\n", value);
    
    // 模拟复杂计算
    int result = value * 3 + 7;
    if (result % 2 == 0) {
        result /= 2;
    } else {
        result = (result + 1) / 2;
    }
    
    char operation_log[100];
    sprintf(operation_log, "额外操作结果: %d", result);
    print_log(operation_log);
}

void print_log(const char *message) {
    // 将消息写入日志文件
    FILE *file = fopen(LOG_FILE, "a");
    if (file == NULL) {
        perror("无法打开日志文件");
        return;
    }
    
    time_t now = time(NULL);
    struct tm *local_time = localtime(&now);
    char time_str[100];
    strftime(time_str, sizeof(time_str), "%Y-%m-%d %H:%M:%S", local_time);
    
    fprintf(file, "[%s] %s\n", time_str, message);
    fclose(file);
}

void generate_report() {
    // 生成报告
    printf("生成报告...\n");
    
    // 模拟报告生成过程
    for (int i = 0; i < 10; i++) {
        perform_extra_operations(i * 5);
    }
    
    char report_message[100];
    sprintf(report_message, "报告生成完成");
    print_log(report_message);
}

void cleanup_resources() {
    // 清理资源
    printf("清理资源...\n");
    
    // 模拟资源清理过程
    for (int i = 0; i < 3; i++) {
        perform_extra_operations(i + 100);
    }
    
    char cleanup_message[100];
    sprintf(cleanup_message, "资源清理完成");
    print_log(cleanup_message);
}
