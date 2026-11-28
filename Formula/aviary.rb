# frozen_string_literal: true

# Installs the Aviary CLI and server from its GitHub release binaries.
class Aviary < Formula
  desc 'Aviary: the AI Agent Nest'
  homepage 'https://aviary.bot'
  license 'MIT'
  version '1.0.16'

  on_macos do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.16/aviary_v1.0.16_darwin_arm64.tar.gz'
      sha256 'b4362170eb65e944a5baf78890ff8ab0fa799163f688ed34bcc16bc21b9ac5cf'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.16/aviary_v1.0.16_darwin_amd64.tar.gz'
      sha256 'c7c1198712a7ffaf838b20a46204fafe63d3591035efb2c3d727e97a9fa4c3ad'
    end
  end

  on_linux do
    on_arm do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.16/aviary_v1.0.16_linux_arm64.tar.gz'
      sha256 '6080466925328e2796a2a8e3906dbe15fe30524654519ba54c9aa1c4ddd65bf2'
    end

    on_intel do
      url 'https://github.com/lsegal/aviary/releases/download/v1.0.16/aviary_v1.0.16_linux_amd64.tar.gz'
      sha256 'e97a9c8500abe72414b976213b8f3107f81b00d436c6c1f67091e29723904244'
    end
  end

  def install
    bin.install 'aviary'
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
