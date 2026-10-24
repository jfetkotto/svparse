module top (input en, input [1:0] s, input [3:0] a, b, output logic [3:0] q, y, w, v, output logic [3:0] bits);
  always_comb begin
    q = 0;
    if (en) q = a;
  end
  always_comb begin
    if (en) y = a; else y = b;
  end
  always @* begin
    case (s)
      0: w = a;
      1: w = b;
      default: w = 0;
    endcase
  end
  always_comb begin
    unique case (s)
      0: v = a;
      1, 2, 3: v = b;
    endcase
  end
  always_comb begin
    logic [3:0] tmp;
    if (en) tmp = a; else tmp = b;
    for (int i = 0; i < 4; i++) bits[i] = tmp[i];
  end
  always_latch if (en) q = a;
endmodule
