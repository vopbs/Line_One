import binascii
from Crypto.Cipher import AES
from Crypto.Util.Padding import unpad, pad

# 对应 PHP 中的硬编码密钥
KEY = '983a6b2c0ce4dfa3d66dc7a2b4092600'

def index_encrypt(value: str) -> str:
    """
    加密函数 (PHP index_decrypt 的逆向操作)
    用于生成测试用的十六进制密文
    """
    try:
        key = binascii.unhexlify(KEY)
        # ECB 模式不需要 IV，默认使用 PKCS7 填充
        cipher = AES.new(key, AES.MODE_ECB)
        # 对明文进行 PKCS7 填充后加密
        encrypted_data = cipher.encrypt(pad(value.encode('utf-8'), AES.block_size))
        # 将二进制结果转换为十六进制字符串返回 (对应 PHP 的 bin2hex)
        return binascii.hexlify(encrypted_data).decode('utf-8')
    except Exception as e:
        print(f"加密异常: {e}")
        return ""

def index_decrypt(value: str) -> str:
    """
    解密函数 (完全对标你的 PHP index_decrypt 方法)
    """
    try:
        key = binascii.unhexlify(KEY)
        cipher = AES.new(key, AES.MODE_ECB)
        
        # 将十六进制字符串转为二进制 (对应 PHP 的 hex2bin)
        raw = binascii.unhexlify(value)
        
        # 解密并去除 PKCS7 填充 (对应 PHP 的 OPENSSL_RAW_DATA)
        decrypted_padded = cipher.decrypt(raw)
        decrypted_data = unpad(decrypted_padded, AES.block_size)
        
        return decrypted_data.decode('utf-8') if decrypted_data else ""
    except Exception as e:
        # 对应 PHP 中的 catch (\Exception $exception) { return ''; }
        print(f"解密异常(可能密文错误或密钥不匹配): {e}")
        return ""


# ================= 测试运行 =================
if __name__ == "__main__":
    test_uid = "600022"
    
    print(f"原始 UID: {test_uid}")
    
    # 1. 模拟前端/调用方加密 uid
    encrypted_uid = index_encrypt(test_uid)
    print(f"加密后的 Hex: {encrypted_uid}")
    
    # 2. 模拟后端接收并解密 uid
    decrypted_uid = index_decrypt(encrypted_uid)
    print(f"解密后的 UID: {decrypted_uid}")
    
    # 3. 测试非法输入 (验证容错机制)
    bad_result = index_decrypt("invalid_hex_string")
    print(f"非法输入测试结果: '{bad_result}'")