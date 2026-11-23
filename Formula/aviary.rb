class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.5"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.5/aviary_v0.7.5_darwin_arm64.tar.gz"
      sha256 "99540138d2d79d27d2eb43dc164a056000d044e108b3fc06d6496e38240321af"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.5/aviary_v0.7.5_darwin_amd64.tar.gz"
      sha256 "5e3a1dd0a9cbec6f1e2aa0f471eaae4fc28ec461c1cd816cf36953a7a27b48e6"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.5/aviary_v0.7.5_linux_arm64.tar.gz"
      sha256 "c49b4633404a9e76e84cda745645c13ddcaba8adb219d4b2e3bb26ad019a35a7"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.5/aviary_v0.7.5_linux_amd64.tar.gz"
      sha256 "4d52a6e25d0c1c83480012be1383a8840b4cdf9c614362afd22c944a1ac5cbfa"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
