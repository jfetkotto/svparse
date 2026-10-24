module top (input [3:0] a, b, c, output logic [3:0] x, y);
  always @(a) begin                // want
    x = a & b;
  end
  always @(a or b) begin           // want
    if (c[0]) y = a; else y = b;
  end
endmodule
