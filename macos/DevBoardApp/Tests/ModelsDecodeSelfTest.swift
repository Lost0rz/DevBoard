import Foundation

@main
struct ModelsDecodeSelfTest {
    static func main() throws {
        let decoder = JSONDecoder()

        let direct = Data(#"{"schemaVersion":1,"ok":true,"status":"installed","message":"ok","data":{"serviceRunning":true,"pid":4242,"binaryPath":"/tmp/devboard"}}"#.utf8)
        let directResult = try decoder.decode(ProductResult.self, from: direct)
        guard directResult.ok, directResult.status == "installed", directResult.providerResults.isEmpty else {
            throw SelfTestError.directResult
        }

        let combined = Data(#"{"schemaVersion":1,"ok":false,"status":"not_configured","data":{"codex":{"schemaVersion":1,"ok":true,"status":"configured_requires_trust","data":{"provider":"codex","configured":true}},"claude-code":{"schemaVersion":1,"ok":false,"status":"not_configured","data":{"provider":"claude-code","configured":false}}}}"#.utf8)
        let combinedResult = try decoder.decode(ProductResult.self, from: combined)
        let providers = combinedResult.providerResults
        guard providers["codex"]?.status == "configured_requires_trust",
              providers["claude-code"]?.status == "not_configured" else {
            throw SelfTestError.combinedResult
        }
    }
}

private enum SelfTestError: Error {
    case directResult
    case combinedResult
}
